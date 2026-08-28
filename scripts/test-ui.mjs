import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const page = await readFile(new URL('../public/index.html', import.meta.url), 'utf8');

for (const match of page.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
  new Function(match[1]);
}

const contracts = [
  ['one shared team filter', 'id="live-team"'],
  ['file preview fallback', "location.protocol !== 'file:'"],
  ['backend fallback team', "id:'backend',name:'Backend-команда'"],
  ['mobile fallback team', "id:'mobile',name:'Mobile-команда'"],
  ['preview team sections', 'const previewTeams = (config.routes || []).map'],
  ['filters hidden in settings', '.rd-controls[hidden] { display:none !important; }'],
  ['hover help', '.rd-info-wrap:hover .rd-info-popover'],
  ['keyboard help', '.rd-info-wrap:focus-within .rd-info-popover'],
  ['ascending and descending sorting', "current.key===header.dataset.sortKey&&current.direction==='desc'?'asc':'desc'"],
  ['daily short ranges', "['week','two_weeks','current_month','previous_month']"],
  ['last week is default', '<option value="week" selected>Последняя неделя</option>'],
  ['work volume API integration', 'fetch(`/api/work-volume?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)'],
  ['daily totals preserved after rounding', 'target-result.reduce((sum,value)=>sum+value,0)'],
  ['daily labels include month', "String(date.getMonth()+1).padStart(2,'0')"],
  ['daily range ends at midnight', 'new Date(today.getFullYear(),today.getMonth(),today.getDate())'],
  ['line chart range padding', 'ownMin-ownRange*.14'],
  ['overall all-teams chart', 'id="rd-overall-chart"'],
  ['one block per configured team', 'id="rd-team-analytics"'],
  ['scrollable employee tables', 'rd-team-table-scroll'],
  ['compact numeric table columns', '.rd-team-table-scroll th:first-child { width:42%; }'],
  ['short-period chart cap', '.rd-line-chart.compact { max-width:720px; margin-inline:auto; }'],
  ['overall frame matches team widget width', '.rd-overall-panel { width:calc((100% - 42px) * .525); margin:0 0 18px; }'],
  ['overall chart is left aligned', '.rd-overall-panel .rd-line-chart { max-width:none; margin-inline:0; }'],
  ['symmetric chart side padding', 'left = 48, right = 48'],
  ['table left edge spacing', 'td:first-child { padding-left:14px; }'],
  ['table right edge spacing', 'td:last-child { padding-right:20px; }'],
  ['employee names are table text', '<span>${escapeHtml(item.name)}</span>'],
  ['changed files line', "name:'Изменено файлов'"],
  ['all metrics use independent visual scales', 'const independentScales = lines.length > 2'],
  ['team totals aggregate employees', "sumSeries(team.people,team.name)"],
];

for (const [name, fragment] of contracts) {
  assert.ok(page.includes(fragment), `${name}: missing ${fragment}`);
}

assert.equal(page.match(/id="live-team"/g)?.length, 1, 'team filter must be shared and unique');
assert.ok(!page.includes('id="rd-volume-person"'), 'employee filter must stay removed');
assert.ok(!page.includes('id="rd-volume-filter"'), 'employee filter chip must stay removed');
assert.ok(!page.includes('class="rd-info-popover" role="tooltip" hidden'), 'help must not depend on click state');
assert.equal(page.match(/\['backend'/g)?.length, 3, 'Backend mock must contain 3 employees');
assert.equal(page.match(/\['mobile'/g)?.length, 15, 'Mobile mock must contain 15 employees');
assert.ok(!page.includes('id="rd-mr-bars"'), 'legacy split MR chart must be removed');
assert.ok(!page.includes('id="rd-lines-bars"'), 'legacy split changes chart must be removed');

console.log(`UI checks passed: ${contracts.length + 7}`);
