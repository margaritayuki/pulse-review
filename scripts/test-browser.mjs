import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { access, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const candidates = process.platform === 'darwin'
  ? ['/Applications/Google Chrome.app/Contents/MacOS/Google Chrome']
  : process.platform === 'win32'
    ? ['C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe','C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe']
    : ['/usr/bin/chromium','/usr/bin/google-chrome'];

async function findBrowser() {
  for (const candidate of [process.env.PULSE_BROWSER_PATH,...candidates].filter(Boolean)) {
    try { await access(candidate); return candidate; } catch {}
  }
  throw new Error('Chromium-браузер не найден. Укажите PULSE_BROWSER_PATH.');
}

const waitFor = async (check,timeout=12000) => {
  const started=Date.now();
  while (Date.now()-started<timeout) {
    try { const value=await check(); if (value) return value; } catch {}
    await new Promise(resolve=>setTimeout(resolve,80));
  }
  throw new Error('Истекло время ожидания браузера');
};

class CDP {
  constructor(url) { this.url=url; this.id=0; this.pending=new Map(); }
  async connect() {
    this.socket=new WebSocket(this.url);
    await new Promise((resolve,reject)=>{ this.socket.onopen=resolve; this.socket.onerror=reject; });
    this.socket.onmessage=event=>{
      const message=JSON.parse(event.data), pending=this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      message.error ? pending.reject(new Error(message.error.message)) : pending.resolve(message.result);
    };
  }
  send(method,params={}) {
    const id=++this.id;
    return new Promise((resolve,reject)=>{ this.pending.set(id,{resolve,reject}); this.socket.send(JSON.stringify({id,method,params})); });
  }
  async evaluate(expression) {
    const response=await this.send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});
    if (response.exceptionDetails) throw new Error(response.exceptionDetails.exception?.description||response.exceptionDetails.text);
    return response.result.value;
  }
  close() { this.socket.close(); }
}

const executablePath=await findBrowser();
const profile=await mkdtemp(join(tmpdir(),'pulse-review-browser-'));
const pageURL=new URL('../public/index.html',import.meta.url).href;
const port=9300+process.pid%500;
const browser=spawn(executablePath,['--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--window-size=1440,900',`--remote-debugging-port=${port}`,`--user-data-dir=${profile}`,pageURL],{stdio:'ignore'});
let cdp;

try {
  const target=await waitFor(async()=>{
    const targets=await fetch(`http://127.0.0.1:${port}/json/list`).then(response=>response.json());
    return targets.find(item=>item.type==='page'&&item.url.startsWith('file:'));
  });
  cdp=new CDP(target.webSocketDebuggerUrl);
  await cdp.connect();
  await cdp.send('Runtime.enable');
  await waitFor(()=>cdp.evaluate('document.readyState === "complete"'));
  await cdp.evaluate(`document.querySelector('[data-view="volume"]').click()`);

  const updateProgressLayout=await cdp.evaluate(`(()=>{const progress=document.querySelector('#rd-update-progress'),dashboard=document.querySelector('[data-page="dashboard"]'),volume=document.querySelector('[data-page="volume"]');progress.hidden=false;const result={width:progress.getBoundingClientRect().width,viewWidth:volume.getBoundingClientRect().width,beforeDashboard:(progress.compareDocumentPosition(dashboard)&Node.DOCUMENT_POSITION_FOLLOWING)!==0,beforeVolume:(progress.compareDocumentPosition(volume)&Node.DOCUMENT_POSITION_FOLLOWING)!==0};document.querySelector('[data-view="volume"]').click();result.visibleInVolume=getComputedStyle(progress).display!=='none';progress.hidden=true;return result})()`);
  assert.ok(Math.abs(updateProgressLayout.width-updateProgressLayout.viewWidth)<=1,JSON.stringify(updateProgressLayout));
  assert.equal(updateProgressLayout.beforeDashboard,true,JSON.stringify(updateProgressLayout));
  assert.equal(updateProgressLayout.beforeVolume,true,JSON.stringify(updateProgressLayout));
  assert.equal(updateProgressLayout.visibleInVolume,true,JSON.stringify(updateProgressLayout));

  const presetControls=await cdp.evaluate(`(()=>{const controls=document.querySelector('#rd-analytics-controls'),period=document.querySelector('#rd-period'),refresh=document.querySelector('#rd-refresh'),main=document.querySelector('.rd-main');return {width:controls.getBoundingClientRect().width,mainWidth:main.getBoundingClientRect().width,gap:refresh.getBoundingClientRect().left-period.getBoundingClientRect().right}})()`);
  assert.ok(presetControls.width<presetControls.mainWidth*.75,JSON.stringify(presetControls));
  assert.ok(Math.abs(presetControls.gap-12)<=1,JSON.stringify(presetControls));
  const controlsSurface=await cdp.evaluate(`(()=>{const style=getComputedStyle(document.querySelector('#rd-analytics-controls'));return {background:style.backgroundColor,border:style.borderTopWidth,padding:style.paddingTop}})()`);
  assert.equal(controlsSurface.background,'rgba(0, 0, 0, 0)',JSON.stringify(controlsSurface));
  assert.equal(controlsSurface.border,'0px',JSON.stringify(controlsSurface));
  assert.equal(controlsSurface.padding,'0px',JSON.stringify(controlsSurface));
  await cdp.evaluate(`document.querySelector('#rd-period').value='custom';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const customControls=await cdp.evaluate(`(async()=>{await new Promise(resolve=>setTimeout(resolve,240));const controls=document.querySelector('#rd-analytics-controls'),to=document.querySelector('#rd-to'),refresh=document.querySelector('#rd-refresh');return {width:controls.getBoundingClientRect().width,gap:refresh.getBoundingClientRect().left-to.getBoundingClientRect().right,custom:controls.classList.contains('rd-custom-period')}})()`);
  assert.ok(customControls.width>presetControls.width,JSON.stringify({presetControls,customControls}));
  assert.ok(Math.abs(customControls.gap-12)<=1,JSON.stringify(customControls));
  assert.equal(customControls.custom,true,JSON.stringify(customControls));
  await cdp.evaluate(`document.querySelector('#rd-period').value='week';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);

  assert.equal(await cdp.evaluate(`document.querySelectorAll('[data-team="backend"] tbody tr').length`),3);
  assert.equal(await cdp.evaluate(`document.querySelectorAll('[data-team="mobile"] tbody tr').length`),15);

  await cdp.evaluate(`document.querySelector('#live-team').value='backend';document.querySelector('#live-team').dispatchEvent(new Event('change',{bubbles:true}))`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-title').textContent`),'Backend-команда');
  assert.equal(await cdp.evaluate(`document.querySelectorAll('.rd-team-analytics-card').length`),1);
  await cdp.evaluate(`document.querySelector('[data-view="dashboard"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelector('#live-team').value`),'backend');
  await cdp.evaluate(`document.querySelector('[data-view="volume"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-title').textContent`),'Backend-команда');
  await cdp.evaluate(`document.querySelector('#live-team').value='all';document.querySelector('#live-team').dispatchEvent(new Event('change',{bubbles:true}))`);

  await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-hit')[2].dispatchEvent(new PointerEvent('pointerenter'))`);
  assert.equal(await cdp.evaluate(`!document.querySelector('#rd-overall-chart .rd-chart-tooltip').hidden`),true);
  const tooltip=await cdp.evaluate(`document.querySelector('#rd-overall-chart .rd-chart-tooltip').innerText`);
  assert.match(tooltip,/Влито MR[\s\S]*Добавлено строк[\s\S]*Удалено строк[\s\S]*Изменено файлов/);
  assert.match(tooltip,/(понедельник|вторник|среда|четверг|пятница|суббота|воскресенье)/i);
  await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-hit')[3].dispatchEvent(new MouseEvent('click',{bubbles:true}));document.querySelectorAll('#rd-overall-chart .rd-line-hit')[3].dispatchEvent(new PointerEvent('pointerleave'))`);
  assert.equal(await cdp.evaluate(`!document.querySelector('#rd-overall-chart .rd-chart-tooltip').hidden`),true);
  await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-hit')[3].dispatchEvent(new MouseEvent('click',{bubbles:true}));document.querySelectorAll('#rd-overall-chart .rd-line-hit')[3].dispatchEvent(new PointerEvent('pointerleave'))`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-chart .rd-chart-tooltip').hidden`),true);

  const firstMR=await cdp.evaluate(`document.querySelector('[data-team="mobile"] tbody tr:first-child td:nth-child(2)').textContent`);
  await cdp.evaluate(`document.querySelector('[data-team="mobile"] th[data-sort-key="mrs"]').click()`);
  const sortedMR=await cdp.evaluate(`document.querySelector('[data-team="mobile"] tbody tr:first-child td:nth-child(2)').textContent`);
  assert.notEqual(sortedMR,firstMR);

  const selectedPerson=await cdp.evaluate(`(()=>{const button=document.querySelector('[data-team="mobile"] .rd-person-filter'),name=button.textContent;button.click();return {name,title:document.querySelector('#rd-overall-title').textContent,cards:document.querySelectorAll('.rd-team-analytics-card').length,pressed:document.querySelector('[data-team="mobile"] .rd-person-filter[aria-pressed="true"]')?.textContent}})()`);
  assert.equal(selectedPerson.title,selectedPerson.name,JSON.stringify(selectedPerson));
  assert.equal(selectedPerson.cards,1,JSON.stringify(selectedPerson));
  assert.equal(selectedPerson.pressed,selectedPerson.name,JSON.stringify(selectedPerson));
  await cdp.evaluate(`document.querySelector('[data-team="mobile"] .rd-person-filter[aria-pressed="true"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-title').textContent`),'Все команды');
  assert.equal(await cdp.evaluate(`document.querySelectorAll('.rd-team-analytics-card').length`),2);

  const scroll=await cdp.evaluate(`(()=>{const e=document.querySelector('[data-team="mobile"] .rd-team-table-scroll');return {scrollHeight:e.scrollHeight,clientHeight:e.clientHeight}})()`);
  assert.ok(scroll.scrollHeight>scroll.clientHeight);
  const widths=await cdp.evaluate(`({scrollWidth:document.body.scrollWidth,clientWidth:document.body.clientWidth})`);
  assert.ok(widths.scrollWidth<=widths.clientWidth+1);
  const gaps=await cdp.evaluate(`(()=>{const body=document.querySelector('[data-team="backend"] .rd-team-analytics-body'),chart=body.querySelector('.rd-team-chart'),table=body.querySelector('.rd-team-table');return {gap:parseFloat(getComputedStyle(body).columnGap),distance:table.getBoundingClientRect().left-chart.getBoundingClientRect().right}})()`);
  assert.ok(gaps.gap>=12&&gaps.distance>=12);

  await cdp.evaluate(`document.querySelector('#rd-period').value='week';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const weeklyChart=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart');return {period:document.querySelector('#rd-period').value,compact:chart.classList.contains('compact'),semiCompact:chart.classList.contains('semi-compact'),width:chart.getBoundingClientRect().width,hits:chart.querySelectorAll('.rd-line-hit').length,labels:[...chart.querySelectorAll('.rd-line-axis')].map(node=>node.textContent).filter(text=>/^\\d{2}\\.\\d{2}$/.test(text))}})()`);
  assert.equal(weeklyChart.compact,true,JSON.stringify(weeklyChart));
  assert.equal(weeklyChart.labels.length,7);
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-axis-date[transform^="rotate(-45"]').length`),7);
  const weeklyCalendar=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart'),svg=chart.querySelector('svg'),height=svg.viewBox.baseVal.height,labels=[...chart.querySelectorAll('.rd-line-axis-date')],weekends=[...chart.querySelectorAll('.rd-line-axis-weekend')];return {weekends:weekends.length,weekendLabels:weekends.map(node=>node.textContent),weekdayColor:getComputedStyle(labels.find(node=>!node.classList.contains('rd-line-axis-weekend'))).fill,weekendColor:getComputedStyle(weekends[0]).fill,inside:labels.every(node=>{const box=node.getBBox();return box.y>=0&&box.y+box.height<=height})}})()`);
  assert.ok(weeklyCalendar.weekends>=2,JSON.stringify(weeklyCalendar));
  assert.ok(weeklyCalendar.weekendLabels.every(label=>/^\d{2}\.\d{2}$/.test(label)),JSON.stringify(weeklyCalendar));
  assert.notEqual(weeklyCalendar.weekdayColor,weeklyCalendar.weekendColor,JSON.stringify(weeklyCalendar));
  assert.equal(weeklyCalendar.inside,true,JSON.stringify(weeklyCalendar));
  const chartScale=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart'),svg=chart.querySelector('svg'),ticks=[...chart.querySelectorAll('.rd-y-axis-tick')].map(node=>node.textContent),paths=[...chart.querySelectorAll('.rd-line-path')].map(node=>node.getAttribute('d'));return {mode:svg.dataset.scaleMode,ticks,axisTitles:chart.querySelectorAll('.rd-line-axis-title').length,distinctPaths:new Set(paths).size,totalPaths:paths.length}})()`);
  assert.equal(chartScale.mode,'nonlinear',JSON.stringify(chartScale));
  assert.ok(chartScale.ticks.length>=5,JSON.stringify(chartScale));
  assert.equal(chartScale.axisTitles,0,JSON.stringify(chartScale));
  assert.equal(chartScale.totalPaths,4,JSON.stringify(chartScale));
  assert.ok(chartScale.distinctPaths>=3,JSON.stringify(chartScale));

  await cdp.evaluate(`(()=>{window.__pulseMetricFetches=0;window.__pulseMetricOriginalFetch=window.fetch;window.fetch=(...args)=>{window.__pulseMetricFetches++;return window.__pulseMetricOriginalFetch(...args)};for(const key of ['mrs','deleted','files'])document.querySelector('#rd-overall-legend [data-metric="'+key+'"]').click()})()`);
  const classicScale=await cdp.evaluate(`(()=>({mode:document.querySelector('#rd-overall-chart svg').dataset.scaleMode,paths:document.querySelectorAll('#rd-overall-chart .rd-line-path').length,selected:[...document.querySelectorAll('#rd-overall-legend .rd-metric-toggle')].filter(button=>button.getAttribute('aria-pressed')==='true').map(button=>button.dataset.metric),fetches:window.__pulseMetricFetches}))()`);
  assert.equal(classicScale.mode,'linear',JSON.stringify(classicScale));
  assert.equal(classicScale.paths,1,JSON.stringify(classicScale));
  assert.deepEqual(classicScale.selected,['added'],JSON.stringify(classicScale));
  assert.equal(classicScale.fetches,0,JSON.stringify(classicScale));
  await cdp.evaluate(`for(const key of ['mrs','deleted','files'])document.querySelector('#rd-overall-legend [data-metric="'+key+'"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-chart svg').dataset.scaleMode`),'nonlinear');
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-path').length`),4);

  await cdp.evaluate(`document.querySelector('[data-chart-key="overall"] .rd-chart-expand').click()`);
  const fullscreen=await cdp.evaluate(`(()=>{const shell=document.querySelector('[data-chart-key="overall"]'),rect=shell.getBoundingClientRect(),button=shell.querySelector('.rd-chart-expand');return {expanded:shell.classList.contains('rd-chart-expanded'),width:rect.width,height:rect.height,viewportWidth:innerWidth,viewportHeight:innerHeight,label:button.getAttribute('aria-label'),locked:document.documentElement.classList.contains('pulse-review-chart-lock')}})()`);
  assert.equal(fullscreen.expanded,true,JSON.stringify(fullscreen));
  assert.ok(fullscreen.width<=1902&&fullscreen.width<=fullscreen.viewportWidth-47,JSON.stringify(fullscreen));
  assert.ok(fullscreen.height<=1080&&fullscreen.height<=fullscreen.viewportHeight-47,JSON.stringify(fullscreen));
  assert.equal(fullscreen.label,'Свернуть график',JSON.stringify(fullscreen));
  assert.equal(fullscreen.locked,true,JSON.stringify(fullscreen));
  await cdp.evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`);
  assert.equal(await cdp.evaluate(`document.querySelector('[data-chart-key="overall"]').classList.contains('rd-chart-expanded')`),false);
  assert.equal(await cdp.evaluate(`document.documentElement.classList.contains('pulse-review-chart-lock')`),false);
  await cdp.evaluate(`window.fetch=window.__pulseMetricOriginalFetch;delete window.__pulseMetricOriginalFetch;delete window.__pulseMetricFetches`);
  await cdp.evaluate(`document.querySelector('#rd-period').value='current_month';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const monthlyDates=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart');return {hits:chart.querySelectorAll('.rd-line-hit').length,labels:chart.querySelectorAll('.rd-line-axis-date[transform^="rotate(-45"]').length}})()`);
  assert.equal(monthlyDates.labels,monthlyDates.hits,JSON.stringify(monthlyDates));
  assert.ok(monthlyDates.labels>=28,JSON.stringify(monthlyDates));
  await cdp.evaluate(`document.querySelector('#rd-period').value='week';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const chartAlignment=await cdp.evaluate(`(()=>{const panel=document.querySelector('.rd-overall-panel'),overall=document.querySelector('#rd-overall-chart'),team=document.querySelector('[data-team="backend"] .rd-team-chart'),teamCard=document.querySelector('[data-team="backend"]');const points=[...overall.querySelectorAll('.rd-line-point')],periods=overall.querySelectorAll('.rd-line-hit').length;return {panelWidth:panel.getBoundingClientRect().width,teamWidth:team.getBoundingClientRect().width,panelLeft:panel.getBoundingClientRect().left,teamLeft:teamCard.getBoundingClientRect().left,firstX:Number(points[0].getAttribute('cx')),lastX:Number(points[periods-1].getAttribute('cx'))}})()`);
  assert.ok(Math.abs(chartAlignment.panelWidth-chartAlignment.teamWidth)<=2,JSON.stringify(chartAlignment));
  assert.ok(Math.abs(chartAlignment.panelLeft-chartAlignment.teamLeft)<=2,JSON.stringify(chartAlignment));
  assert.equal(chartAlignment.firstX,48);
  assert.equal(chartAlignment.lastX,592);
  const edgeSpacing=await cdp.evaluate(`(()=>{const table=document.querySelector('[data-team="backend"] table'),first=table.querySelector('tbody td:first-child'),last=table.querySelector('tbody td:last-child'),lastHead=table.querySelector('thead th:last-child');return {left:parseFloat(getComputedStyle(first).paddingLeft),right:parseFloat(getComputedStyle(last).paddingRight),headerRight:parseFloat(getComputedStyle(lastHead).paddingRight)}})()`);
  assert.ok(edgeSpacing.left>=14);
  assert.ok(edgeSpacing.right>=20);
  assert.ok(edgeSpacing.headerRight>=20);

  await cdp.evaluate(`(()=>{window.__pulseOriginalFetch=window.fetch;window.fetch=(url,options={})=>String(url).startsWith('/api/report')?new Promise((resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true})):window.__pulseOriginalFetch(url,options);document.querySelector('#rd-refresh').click()})()`);
  await waitFor(()=>cdp.evaluate(`!document.querySelector('#rd-update-progress').hidden`));
  const cancelControl=await cdp.evaluate(`(()=>{const button=document.querySelector('#rd-update-cancel');return {tooltip:button.dataset.tooltip,count:document.querySelector('#rd-update-progress-count').textContent}})()`);
  assert.equal(cancelControl.tooltip,'Обновление данных будет отменено');
  assert.ok(cancelControl.count.length>0,JSON.stringify(cancelControl));
  await cdp.evaluate(`document.querySelector('#rd-update-cancel').click()`);
  await waitFor(()=>cdp.evaluate(`document.querySelector('#rd-update-progress').hidden`));
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-toast').textContent`),'Обновление отменено');
  await cdp.evaluate(`window.fetch=window.__pulseOriginalFetch;delete window.__pulseOriginalFetch`);

  console.log('Browser checks passed: 36');
} finally {
  cdp?.close();
  browser.kill('SIGTERM');
  if (browser.exitCode===null) await Promise.race([once(browser,'exit'),new Promise(resolve=>setTimeout(resolve,2000))]);
  for (let attempt=0;attempt<4;attempt++) {
    try { await rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:80}); break; }
    catch (error) { if (attempt===3) throw error; await new Promise(resolve=>setTimeout(resolve,120)); }
  }
}
