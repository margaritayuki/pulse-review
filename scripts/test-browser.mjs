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
const browser=spawn(executablePath,['--headless=new','--disable-gpu','--disable-extensions','--disable-crash-reporter','--disable-breakpad','--noerrdialogs','--no-first-run','--no-default-browser-check','--window-size=1440,900',`--remote-debugging-port=${port}`,`--user-data-dir=${profile}`,pageURL],{stdio:'ignore'});
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
  const dashboardInitial=await cdp.evaluate(`(()=>({active:document.querySelector('.rd-tab.active')?.dataset.view,overviewVisible:!document.querySelector('[data-page="overview"]').hidden,reviewHidden:document.querySelector('[data-page="dashboard"]').hidden,cards:document.querySelectorAll('#rd-dashboard-cards > .rd-panel').length,overallLines:document.querySelectorAll('#rd-dashboard-overall-chart .rd-line-path').length,periodRows:document.querySelectorAll('#rd-dashboard-periods .rd-dashboard-period-row').length,reviewTitle:document.querySelector('[data-page="dashboard"] h1').textContent}))()`);
  assert.equal(dashboardInitial.active,'overview',JSON.stringify(dashboardInitial));
  assert.equal(dashboardInitial.overviewVisible,true,JSON.stringify(dashboardInitial));
  assert.equal(dashboardInitial.reviewHidden,true,JSON.stringify(dashboardInitial));
  assert.equal(dashboardInitial.cards,4,JSON.stringify(dashboardInitial));
  assert.equal(dashboardInitial.overallLines,4,JSON.stringify(dashboardInitial));
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-dashboard-overall-chart .rd-line-area').length`),4);
  assert.ok(await cdp.evaluate(`(()=>[...document.querySelectorAll('#rd-dashboard-cards .rd-dashboard-card-chart')].every(chart=>chart.querySelectorAll('.rd-line-area').length===chart.querySelectorAll('.rd-line-path').length))()`));
  assert.ok(dashboardInitial.periodRows>0,JSON.stringify(dashboardInitial));
  assert.equal(dashboardInitial.reviewTitle,'Ревью команды',JSON.stringify(dashboardInitial));
  const dashboardAxisSpacing=await cdp.evaluate(`(()=>{const svg=document.querySelector('#rd-dashboard-cards .rd-dashboard-card-chart svg'),tick=svg.querySelector('.rd-y-axis-tick'),grid=svg.querySelector('.rd-line-grid'),box=tick.getBBox(),width=svg.viewBox.baseVal.width;return {tickLeft:box.x,plotLeft:Number(grid.getAttribute('x1')),plotRightGap:width-Number(grid.getAttribute('x2'))}})()`);
  assert.ok(dashboardAxisSpacing.tickLeft>=8,JSON.stringify(dashboardAxisSpacing));
  assert.ok(dashboardAxisSpacing.plotLeft>dashboardAxisSpacing.plotRightGap,JSON.stringify(dashboardAxisSpacing));
  const dashboardLegends=await cdp.evaluate(`(()=>{const describe=id=>[...document.querySelectorAll(id+' .rd-metric-toggle')].map(button=>({text:button.textContent.trim(),dot:!!button.querySelector('i'),pressed:button.getAttribute('aria-pressed')}));return {overall:describe('#rd-dashboard-overall-legend'),period:describe('#rd-dashboard-period-legend')}})()`);
  assert.deepEqual(dashboardLegends.overall,dashboardLegends.period,JSON.stringify(dashboardLegends));
  assert.equal(dashboardLegends.overall.length,4,JSON.stringify(dashboardLegends));
  assert.ok(dashboardLegends.overall.every(item=>item.dot&&item.pressed==='true'),JSON.stringify(dashboardLegends));
  const dashboardTypography=await cdp.evaluate(`(()=>{const title=document.querySelector('.rd-dashboard-card-title span:first-child'),total=document.querySelector('.rd-dashboard-card-total'),head=document.querySelector('.rd-dashboard-breakdown-head');return {titleSize:getComputedStyle(title).fontSize,totalSize:getComputedStyle(total).fontSize,titleWeight:getComputedStyle(title).fontWeight,totalWeight:getComputedStyle(total).fontWeight,breakdownLeft:head.getBoundingClientRect().left,cardsLeft:document.querySelector('#rd-dashboard-cards').getBoundingClientRect().left}})()`);
  assert.equal(dashboardTypography.titleSize,dashboardTypography.totalSize,JSON.stringify(dashboardTypography));
  assert.equal(dashboardTypography.titleWeight,dashboardTypography.totalWeight,JSON.stringify(dashboardTypography));
  assert.ok(Math.abs(dashboardTypography.breakdownLeft-dashboardTypography.cardsLeft)<=1,JSON.stringify(dashboardTypography));
  const dashboardMetrics=['mrs','added','deleted','files'];
  const dashboardGroupKeys={'Дни':'days','Недели':'weeks','Месяцы':'months','Кварталы':'quarters'};
  const formatISO=date=>date.toISOString().slice(0,10);
  const customRange=days=>{
    const to=new Date(Date.UTC(2026,7,28)),from=new Date(to);
    from.setUTCDate(from.getUTCDate()-days+1);
    return {from:formatISO(from),to:formatISO(to)};
  };
  const customLabels=(range,group)=>{
    const dates=[];
    for (let date=new Date(`${range.from}T00:00:00Z`),to=new Date(`${range.to}T00:00:00Z`);date<=to;date=new Date(date.getTime()+86400000)) dates.push(date);
    const day=date=>`${String(date.getUTCDate()).padStart(2,'0')}.${String(date.getUTCMonth()+1).padStart(2,'0')}`;
    if (group==='Дни') return dates.map(day);
    if (group==='Недели') return Array.from({length:Math.ceil(dates.length/7)},(_,index)=>`${day(dates[index*7])}–${day(dates[Math.min(dates.length-1,index*7+6)])}`);
    if (group==='Месяцы') return [...new Set(dates.map(date=>`${String(date.getUTCMonth()+1).padStart(2,'0')}'${String(date.getUTCFullYear()).slice(-2)}`))];
    return [...new Set(dates.map(date=>`${Math.floor(date.getUTCMonth()/3)+1}q'${String(date.getUTCFullYear()).slice(-2)}`))];
  };
  const setDashboardPeriod=async testCase=>{
    await cdp.evaluate(`(()=>{const period=document.querySelector('#rd-period');period.value=${JSON.stringify(testCase.period)};period.dispatchEvent(new Event('change',{bubbles:true}));${testCase.range ? `const from=document.querySelector('#rd-from'),to=document.querySelector('#rd-to');from.value=${JSON.stringify(testCase.range.from)};to.value=${JSON.stringify(testCase.range.to)};from.dispatchEvent(new Event('change',{bubbles:true}));to.dispatchEvent(new Event('change',{bubbles:true}));` : ''}})()`);
  };
  const setDashboardView=async view=>{
    await cdp.evaluate(`document.querySelector('[data-dashboard-view=${JSON.stringify(view)}]').click()`);
    const state=await cdp.evaluate(`(()=>({active:document.querySelector('[data-dashboard-view=${JSON.stringify(view)}]').getAttribute('aria-pressed'),lineCharts:document.querySelectorAll('#rd-dashboard-cards .rd-dashboard-card-chart').length,barCharts:document.querySelectorAll('#rd-dashboard-cards .rd-dashboard-card-bars').length,cards:document.querySelectorAll('#rd-dashboard-cards > .rd-panel').length}))()`);
    assert.equal(state.active,'true',JSON.stringify({view,state}));
    assert.equal(state.cards,4,JSON.stringify({view,state}));
    assert.equal(state.lineCharts,view==='line'?4:0,JSON.stringify({view,state}));
    assert.equal(state.barCharts,view==='bar'?4:0,JSON.stringify({view,state}));
  };
  const selectDashboardGroup=async group=>{
    for (const metric of dashboardMetrics) {
      const clicked=await cdp.evaluate(`(()=>{const button=document.querySelector('[data-dashboard-metric=${JSON.stringify(metric)}][data-dashboard-group=${JSON.stringify(dashboardGroupKeys[group])}]');if(!button)return false;button.click();return true})()`);
      assert.equal(clicked,true,JSON.stringify({metric,group,error:'Не найдена кнопка группировки'}));
    }
  };
  const readDashboardBars=()=>cdp.evaluate(`(()=>{const number=text=>Number(String(text).replace(/[^0-9-]/g,''));return [...document.querySelectorAll('#rd-dashboard-cards > .rd-panel')].map(card=>{const title=card.querySelector('.rd-dashboard-card-title span:first-child').textContent.trim(),key=({"Влито MR":"mrs","Добавлено строк":"added","Удалено строк":"deleted","Изменено файлов":"files"})[title],buttons=[...card.querySelectorAll('.rd-dashboard-grouping button')],rows=[...card.querySelectorAll('.rd-dashboard-bar-row')].map(row=>({label:row.querySelector('.rd-dashboard-period-label').textContent.trim(),value:number(row.querySelector('.rd-dashboard-bar-value').textContent)}));return {key,title,total:number(card.querySelector('.rd-dashboard-card-total').textContent),groups:[...card.querySelectorAll('.rd-dashboard-grouping button,.rd-dashboard-grouping span')].map(item=>item.textContent.trim()),active:buttons.filter(button=>button.getAttribute('aria-pressed')==='true').map(button=>button.textContent.trim()),rows}})})()`);
  const groupingCases=[
    {name:'последняя неделя',period:'week',groups:['Дни'],labels:{'Дни':/^\d{2}\.\d{2}$/}},
    {name:'последние 2 недели',period:'two_weeks',groups:['Дни','Недели'],labels:{'Дни':/^\d{2}\.\d{2}$/,'Недели':/^\d{2}\.\d{2}–\d{2}\.\d{2}$/}},
    {name:'текущий месяц',period:'current_month',groups:['Дни','Недели'],labels:{'Дни':/^\d{2}\.\d{2}$/,'Недели':/^\d{2}\.\d{2}–\d{2}\.\d{2}$/}},
    {name:'прошлый месяц',period:'previous_month',groups:['Дни','Недели'],labels:{'Дни':/^\d{2}\.\d{2}$/,'Недели':/^\d{2}\.\d{2}–\d{2}\.\d{2}$/}},
    {name:'текущий квартал',period:'current_quarter',groups:['Недели','Месяцы'],labels:{'Недели':/^\d{2}\.\d{2}–\d{2}\.\d{2}$/,'Месяцы':/^\d{2}'\d{2}$/}},
    {name:'прошлый квартал',period:'previous_quarter',groups:['Недели','Месяцы'],labels:{'Недели':/^\d{2}\.\d{2}–\d{2}\.\d{2}$/,'Месяцы':/^\d{2}'\d{2}$/}},
    {name:'последние полгода',period:'half_year',groups:['Месяцы','Кварталы'],labels:{'Месяцы':/^\d{2}'\d{2}$/,'Кварталы':/^\dq'\d{2}$/}},
    {name:'последний год',period:'year',groups:['Месяцы','Кварталы'],labels:{'Месяцы':/^\d{2}'\d{2}$/,'Кварталы':/^\dq'\d{2}$/}},
    ...[
      [7,['Дни','Недели']], [14,['Дни','Недели']], [31,['Дни','Недели']],
      [32,['Недели','Месяцы']], [93,['Недели','Месяцы']],
      [94,['Месяцы','Кварталы']], [180,['Месяцы','Кварталы']], [365,['Месяцы','Кварталы']]
    ].map(([days,groups])=>{const range=customRange(days);return {
      name:`свои даты — ${days} дней`,period:'custom',range,groups,
      labels:Object.fromEntries(groups.map(group=>[group,group==='Дни'?/^\d{2}\.\d{2}$/:group==='Недели'?/^\d{2}\.\d{2}–\d{2}\.\d{2}$/:group==='Месяцы'?/^\d{2}'\d{2}$/:/^\dq'\d{2}$/])),
      expectedLabels:Object.fromEntries(groups.map(group=>[group,customLabels(range,group)]))
    }})
  ];
  await cdp.evaluate(`window.__dashboardOriginalFetch=window.fetch;window.__dashboardFetches=0;window.fetch=(...args)=>{window.__dashboardFetches++;return window.__dashboardOriginalFetch(...args)}`);
  const groupingFailures=[];
  for (const testCase of groupingCases) {
    await setDashboardPeriod(testCase);
    await setDashboardView('line');
    const lineState=await cdp.evaluate(`(()=>[...document.querySelectorAll('#rd-dashboard-cards .rd-dashboard-card-chart')].map(chart=>({paths:chart.querySelectorAll('.rd-line-path').length,areas:chart.querySelectorAll('.rd-line-area').length,points:chart.querySelectorAll('.rd-line-point').length})))()`);
    assert.ok(lineState.every(card=>card.paths===1&&card.areas===1&&card.points>0),JSON.stringify({case:testCase.name,view:'Линейный',lineState}));
    await setDashboardView('bar');
    if (testCase.period==='week') assert.ok(await cdp.evaluate(`document.querySelector('#rd-dashboard-cards .rd-dashboard-card-bars').getBoundingClientRect().height<=202`),JSON.stringify({case:testCase.name,view:'Линейчатый'}));
    let cards=await readDashboardBars();
    assert.ok(cards.every(card=>JSON.stringify(card.groups)===JSON.stringify(testCase.groups)),JSON.stringify({case:testCase.name,expected:testCase.groups,cards}));
    const expectedTotals=Object.fromEntries(cards.map(card=>[card.key,card.total]));
    for (const group of testCase.groups) {
      if (testCase.groups.length>1) await selectDashboardGroup(group);
      cards=await readDashboardBars();
      for (const card of cards) {
        assert.equal(card.total,expectedTotals[card.key],JSON.stringify({case:testCase.name,group,metric:card.title,error:'Итог изменился при смене группировки',card}));
        assert.equal(card.rows.reduce((sum,row)=>sum+row.value,0),card.total,JSON.stringify({case:testCase.name,group,metric:card.title,error:'Сумма интервалов не равна итогу',card}));
        assert.ok(card.rows.length>0,JSON.stringify({case:testCase.name,group,metric:card.title,error:'Нет интервалов'}));
        assert.ok(card.rows.every(row=>row.value>0),JSON.stringify({case:testCase.name,group,metric:card.title,error:'Нулевой интервал не должен отображаться',rows:card.rows}));
        const actualLabels=card.rows.map(row=>row.label),formatValid=actualLabels.every(label=>testCase.labels[group].test(label)),expectedLabels=testCase.expectedLabels?.[group];
        let expectedIndex=-1;
        const rangeValid=!expectedLabels||actualLabels.every(label=>{expectedIndex=expectedLabels.indexOf(label,expectedIndex+1);return expectedIndex>=0;});
        if (!formatValid||!rangeValid) {
          const existing=groupingFailures.find(item=>item.case===testCase.name&&item.group===group);
          if (existing) existing.metrics.push(card.title);
          else {
            const summarize=labels=>labels&&({count:labels.length,first:labels[0],last:labels.at(-1)});
            groupingFailures.push({case:testCase.name,group,metrics:[card.title],error:formatValid?'Интервалы не соответствуют выбранным датам':'Неверный формат подписи',expected:summarize(testCase.expectedLabels?.[group]),actual:summarize(actualLabels)});
          }
        }
        if (testCase.groups.length>1) assert.deepEqual(card.active,[group],JSON.stringify({case:testCase.name,group,metric:card.title,error:'Неверная активная группировка',card}));
        else assert.deepEqual(card.active,[],JSON.stringify({case:testCase.name,group,metric:card.title,error:'Для единственной группировки не нужна кнопка',card}));
      }
    }
  }
  // У backend за неделю мок даёт несколько дней с 0 MR. Они должны быть
  // полностью исключены из расшифровки, но итог карточки обязан сохраниться.
  await cdp.evaluate(`(()=>{const team=document.querySelector('#live-team');team.value='backend';team.dispatchEvent(new Event('change',{bubbles:true}));const period=document.querySelector('#rd-period');period.value='week';period.dispatchEvent(new Event('change',{bubbles:true}));document.querySelector('[data-dashboard-view="bar"]').click()})()`);
  const zeroPeriodState=await cdp.evaluate(`(()=>{const card=[...document.querySelectorAll('#rd-dashboard-cards > .rd-panel')].find(item=>item.querySelector('.rd-dashboard-card-title').textContent.includes('Влито MR')),number=text=>Number(String(text).replace(/[^0-9-]/g,'')),values=[...card.querySelectorAll('.rd-dashboard-bar-value')].map(item=>number(item.textContent));return {total:number(card.querySelector('.rd-dashboard-card-total').textContent),values,labels:[...card.querySelectorAll('.rd-dashboard-period-label')].map(item=>item.textContent.trim())}})()`);
  assert.ok(zeroPeriodState.values.length>0&&zeroPeriodState.values.length<7,JSON.stringify({error:'Нулевые дневные интервалы MR не отфильтрованы',zeroPeriodState}));
  assert.ok(zeroPeriodState.values.every(value=>value>0),JSON.stringify({error:'В карточке остался нулевой интервал',zeroPeriodState}));
  assert.equal(zeroPeriodState.values.reduce((sum,value)=>sum+value,0),zeroPeriodState.total,JSON.stringify({error:'После фильтрации нулей изменился итог',zeroPeriodState}));
  await cdp.evaluate(`(()=>{const team=document.querySelector('#live-team');team.value='all';team.dispatchEvent(new Event('change',{bubbles:true}))})()`);
  if (groupingFailures.length) assert.fail(JSON.stringify({error:'Подписи группировок не соответствуют выбранному периоду',failures:groupingFailures}));
  await cdp.evaluate(`document.querySelector('#rd-dashboard-overall-legend [data-dashboard-period-metric="files"]').click()`);
  const toggledDashboardLegends=await cdp.evaluate(`(()=>({overall:document.querySelector('#rd-dashboard-overall-legend [data-dashboard-period-metric="files"]').getAttribute('aria-pressed'),period:document.querySelector('#rd-dashboard-period-legend [data-dashboard-period-metric="files"]').getAttribute('aria-pressed'),overallLines:document.querySelectorAll('#rd-dashboard-overall-chart .rd-line-path').length}))()`);
  assert.deepEqual(toggledDashboardLegends,{overall:'false',period:'false',overallLines:3},JSON.stringify(toggledDashboardLegends));
  await cdp.evaluate(`document.querySelector('#rd-period').value='two_weeks';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-dashboard-cards [data-dashboard-group]').length`),8);
  await cdp.evaluate(`document.querySelector('#rd-period').value='week';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}));document.querySelector('[data-dashboard-view="line"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-dashboard-cards .rd-dashboard-grouping button').length`),0);
  assert.equal(await cdp.evaluate(`window.__dashboardFetches`),0,'Локальные переключения Дашборда не должны выполнять fetch');
  await cdp.evaluate(`window.fetch=window.__dashboardOriginalFetch;delete window.__dashboardOriginalFetch;delete window.__dashboardFetches`);
  await cdp.evaluate(`document.querySelector('[data-view="volume"]').click()`);
  assert.ok(await cdp.evaluate(`(()=>[...document.querySelectorAll('[data-page="volume"] .rd-line-chart')].every(chart=>chart.querySelectorAll('.rd-line-area').length===chart.querySelectorAll('.rd-line-path').length))()`));

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
  const controlChrome=await cdp.evaluate(`(()=>{const refresh=getComputedStyle(document.querySelector('#rd-refresh')),select=getComputedStyle(document.querySelector('#live-team'));return {refreshBorder:refresh.borderTopWidth,refreshBackground:refresh.backgroundColor,refreshColor:refresh.color,selectPaddingRight:parseFloat(select.paddingRight),selectBackgroundPosition:select.backgroundPosition}})()`);
  assert.equal(controlChrome.refreshBorder,'0px',JSON.stringify(controlChrome));
  assert.equal(controlChrome.refreshBackground,'rgba(0, 0, 0, 0)',JSON.stringify(controlChrome));
  assert.match(controlChrome.refreshColor,/89, 101, 216|5965d8/i,JSON.stringify(controlChrome));
  assert.ok(controlChrome.selectPaddingRight>=36,JSON.stringify(controlChrome));
  assert.match(controlChrome.selectBackgroundPosition,/right 12px|calc\(100% - 12px\)/,JSON.stringify(controlChrome));
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
  assert.equal(await cdp.evaluate(`getComputedStyle(document.querySelector('#rd-overall-legend [aria-pressed="false"]')).textDecorationLine`),'none');
  await cdp.evaluate(`for(const key of ['mrs','deleted','files'])document.querySelector('#rd-overall-legend [data-metric="'+key+'"]').click()`);
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-overall-chart svg').dataset.scaleMode`),'nonlinear');
  assert.equal(await cdp.evaluate(`document.querySelectorAll('#rd-overall-chart .rd-line-path').length`),4);

  const normalChartText=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart'),svg=chart.querySelector('svg'),label=chart.querySelector('.rd-line-axis-date'),button=chart.closest('.rd-chart-shell').querySelector('.rd-chart-expand');return {fontSize:getComputedStyle(label).fontSize,viewWidth:svg.viewBox.baseVal.width,clientWidth:svg.getBoundingClientRect().width,buttonBorder:getComputedStyle(button).borderTopWidth}})()`);
  assert.equal(normalChartText.buttonBorder,'0px',JSON.stringify(normalChartText));
  await cdp.evaluate(`document.querySelector('[data-chart-key="overall"] .rd-chart-expand').click()`);
  const fullscreen=await cdp.evaluate(`(()=>{const shell=document.querySelector('[data-chart-key="overall"]'),rect=shell.getBoundingClientRect(),button=shell.querySelector('.rd-chart-expand'),svg=shell.querySelector('svg'),label=shell.querySelector('.rd-line-axis-date');return {expanded:shell.classList.contains('rd-chart-expanded'),width:rect.width,height:rect.height,viewportWidth:innerWidth,viewportHeight:innerHeight,label:button.getAttribute('aria-label'),locked:document.documentElement.classList.contains('pulse-review-chart-lock'),fontSize:getComputedStyle(label).fontSize,viewWidth:svg.viewBox.baseVal.width,clientWidth:svg.getBoundingClientRect().width}})()`);
  assert.equal(fullscreen.expanded,true,JSON.stringify(fullscreen));
  assert.ok(fullscreen.width<=1902&&fullscreen.width<=fullscreen.viewportWidth-47,JSON.stringify(fullscreen));
  assert.ok(fullscreen.height<=1080&&fullscreen.height<=fullscreen.viewportHeight-47,JSON.stringify(fullscreen));
  assert.equal(fullscreen.label,'Свернуть график',JSON.stringify(fullscreen));
  assert.equal(fullscreen.locked,true,JSON.stringify(fullscreen));
  assert.equal(fullscreen.fontSize,normalChartText.fontSize,JSON.stringify({normalChartText,fullscreen}));
  assert.ok(Math.abs(fullscreen.viewWidth-fullscreen.clientWidth)<=2,JSON.stringify(fullscreen));
  await cdp.evaluate(`document.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`);
  assert.equal(await cdp.evaluate(`document.querySelector('[data-chart-key="overall"]').classList.contains('rd-chart-expanded')`),false);
  assert.equal(await cdp.evaluate(`document.documentElement.classList.contains('pulse-review-chart-lock')`),false);
  await cdp.evaluate(`window.fetch=window.__pulseMetricOriginalFetch;delete window.__pulseMetricOriginalFetch;delete window.__pulseMetricFetches`);
  await cdp.evaluate(`document.querySelector('#rd-period').value='current_month';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const monthlyDates=await cdp.evaluate(`(()=>{const chart=document.querySelector('#rd-overall-chart');return {hits:chart.querySelectorAll('.rd-line-hit').length,labels:chart.querySelectorAll('.rd-line-axis-date[transform^="rotate(-45"]').length}})()`);
  assert.equal(monthlyDates.labels,monthlyDates.hits,JSON.stringify(monthlyDates));
  assert.ok(monthlyDates.labels>=28,JSON.stringify(monthlyDates));
  await cdp.evaluate(`document.querySelector('#rd-period').value='week';document.querySelector('#rd-period').dispatchEvent(new Event('change',{bubbles:true}))`);
  const chartAlignment=await cdp.evaluate(`(()=>{const panel=document.querySelector('.rd-overall-panel'),overall=document.querySelector('#rd-overall-chart'),svg=overall.querySelector('svg'),team=document.querySelector('[data-team="backend"] .rd-team-chart'),teamCard=document.querySelector('[data-team="backend"]');const points=[...overall.querySelectorAll('.rd-line-point')],periods=overall.querySelectorAll('.rd-line-hit').length;return {panelWidth:panel.getBoundingClientRect().width,teamWidth:team.getBoundingClientRect().width,panelLeft:panel.getBoundingClientRect().left,teamLeft:teamCard.getBoundingClientRect().left,viewWidth:svg.viewBox.baseVal.width,firstX:Number(points[0].getAttribute('cx')),lastX:Number(points[periods-1].getAttribute('cx'))}})()`);
  assert.ok(Math.abs(chartAlignment.panelWidth-chartAlignment.teamWidth)<=2,JSON.stringify(chartAlignment));
  assert.ok(Math.abs(chartAlignment.panelLeft-chartAlignment.teamLeft)<=2,JSON.stringify(chartAlignment));
  assert.equal(chartAlignment.firstX,68);
  assert.equal(chartAlignment.viewWidth-chartAlignment.lastX,28);
  const nonNegativeCurves=await cdp.evaluate(`(()=>[...document.querySelectorAll('.rd-line-chart svg')].every(svg=>{const zero=Number(svg.dataset.zeroY);return [...svg.querySelectorAll('.rd-line-path')].every(path=>path.getBBox().y+path.getBBox().height<=zero+1)}))()`);
  assert.equal(nonNegativeCurves,true);
  const edgeSpacing=await cdp.evaluate(`(()=>{const table=document.querySelector('[data-team="backend"] table'),first=table.querySelector('tbody td:first-child'),last=table.querySelector('tbody td:last-child'),lastHead=table.querySelector('thead th:last-child');return {left:parseFloat(getComputedStyle(first).paddingLeft),right:parseFloat(getComputedStyle(last).paddingRight),headerRight:parseFloat(getComputedStyle(lastHead).paddingRight)}})()`);
  assert.ok(edgeSpacing.left>=14);
  assert.ok(edgeSpacing.right>=20);
  assert.ok(edgeSpacing.headerRight>=20);

  await cdp.evaluate(`document.querySelector('[data-view="settings"]').click()`);
  const settingsChrome=await cdp.evaluate(`(()=>{const remove=document.querySelector('.rd-icon-action'),help=document.querySelector('[aria-label="Как работает персональный сигнал"]'),popover=help.nextElementSibling;help.focus();return {removeBorder:getComputedStyle(remove).borderTopWidth,saveText:document.querySelector('#rd-save-analytics').textContent.trim(),popoverVisible:getComputedStyle(popover).display!=='none',popoverText:popover.textContent.trim()}})()`);
  assert.equal(settingsChrome.removeBorder,'0px',JSON.stringify(settingsChrome));
  assert.equal(settingsChrome.saveText,'Сохранить',JSON.stringify(settingsChrome));
  assert.equal(settingsChrome.popoverVisible,true,JSON.stringify(settingsChrome));
  assert.match(settingsChrome.popoverText,/личной медианы/,JSON.stringify(settingsChrome));
  await cdp.evaluate(`document.querySelector('[data-view="volume"]').click()`);

  await cdp.evaluate(`(()=>{window.__pulseOriginalFetch=window.fetch;window.fetch=(url,options={})=>String(url).startsWith('/api/report')?new Promise((resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true})):window.__pulseOriginalFetch(url,options);document.querySelector('#rd-refresh').click()})()`);
  await waitFor(()=>cdp.evaluate(`!document.querySelector('#rd-update-progress').hidden`));
  const cancelControl=await cdp.evaluate(`(()=>{const button=document.querySelector('#rd-update-cancel');return {tooltip:button.dataset.tooltip,count:document.querySelector('#rd-update-progress-count').textContent}})()`);
  assert.equal(cancelControl.tooltip,'Обновление данных будет отменено');
  assert.ok(cancelControl.count.length>0,JSON.stringify(cancelControl));
  await cdp.evaluate(`document.querySelector('#rd-update-cancel').click()`);
  await waitFor(()=>cdp.evaluate(`document.querySelector('#rd-update-progress').hidden`));
  assert.equal(await cdp.evaluate(`document.querySelector('#rd-toast').textContent`),'Обновление отменено');
  await cdp.evaluate(`window.fetch=window.__pulseOriginalFetch;delete window.__pulseOriginalFetch`);

  console.log(`Browser checks passed: 49 core + ${groupingCases.length} dashboard grouping cases`);
} finally {
  cdp?.close();
  const exited=()=>browser.exitCode!==null||browser.signalCode!==null;
  const waitForExit=timeout=>exited()?Promise.resolve(true):Promise.race([once(browser,'exit').then(()=>true),new Promise(resolve=>setTimeout(()=>resolve(false),timeout))]);
  if (!exited()) browser.kill('SIGTERM');
  if (!await waitForExit(2000)) {
    browser.kill('SIGKILL');
    await waitForExit(500);
  }
  for (let attempt=0;attempt<4;attempt++) {
    try { await rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:80}); break; }
    catch (error) { if (attempt===3) throw error; await new Promise(resolve=>setTimeout(resolve,120)); }
  }
}
