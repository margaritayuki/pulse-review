import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const page = await readFile(new URL('../public/index.html', import.meta.url), 'utf8');

for (const match of page.matchAll(/<script>([\s\S]*?)<\/script>/g)) {
  new Function(match[1]);
}

const contracts = [
  ['one shared team filter', 'id="live-team"'],
  ['file preview fallback', "location.protocol !== 'file:'"],
  ['logo works in file preview', 'src="./pulse-review-mark.svg"'],
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
  ['daily dates are shown in full', "months:dates.map(date =>"],
  ['daily date labels are angled', 'transform="rotate(-45'],
  ['weekday dates are purple', '.rd-line-axis-date { fill:#5965d8;'],
  ['weekend dates are neutral', '.rd-line-axis-weekend { fill:light-dark('],
  ['weekday is present in tooltip', "weekday:'long'"],
  ['employee rows filter dynamics', 'class="rd-person-filter"'],
  ['full-width update progress', 'class="rd-update-progress" id="rd-update-progress"'],
  ['shared update progress precedes dashboard', 'id="rd-update-progress" aria-live="polite"'],
  ['compact preset filters', '.rd-controls { width:min(100%,572px);'],
  ['filter controls have no backing panel', 'padding:0; border:0; background:transparent;'],
  ['expanded custom-date filters', '.rd-controls.rd-custom-period { width:min(100%,1100px);'],
  ['update can be cancelled', "activeReportController?.abort()"],
  ['cancel update hover explanation', 'data-tooltip="Обновление данных будет отменено"'],
  ['report remains visible while loading', "updateProgress.hidden = false"],
  ['daily range ends at midnight', 'new Date(today.getFullYear(),today.getMonth(),today.getDate())'],
  ['tenfold nonlinear scale threshold', 'maximum/minimum>=10'],
  ['classic scale below threshold', "mode:'linear'"],
  ['nonlinear scale for large gaps', "mode:'nonlinear'"],
  ['overall all-teams chart', 'id="rd-overall-chart"'],
  ['one block per configured team', 'id="rd-team-analytics"'],
  ['scrollable employee tables', 'rd-team-table-scroll'],
  ['compact numeric table columns', '.rd-team-table-scroll th:first-child { width:42%; }'],
  ['short-period chart cap', '.rd-line-chart.compact { max-width:720px; margin-inline:auto; }'],
  ['overall frame matches team widget width', '.rd-overall-panel { width:calc((100% - 42px) * .525); margin:0 0 18px; }'],
  ['overall chart is left aligned', '.rd-overall-panel .rd-line-chart { max-width:none; margin-inline:0; }'],
  ['chart padding reserves room for Y-axis labels', 'left = 68, right = 28'],
  ['table left edge spacing', 'td:first-child { padding-left:14px; }'],
  ['table right edge spacing', 'td:last-child { padding-right:20px; }'],
  ['employee names are filter buttons', 'aria-pressed="${active}">${escapeHtml(item.name)}</button>'],
  ['changed files line', "name:'Изменено файлов'"],
  ['one shared real-value scale', 'buildChartScale(visibleLines)'],
  ['metric legend controls', 'rd-metric-toggle'],
  ['fullscreen chart control', 'rd-chart-expand'],
  ['fullscreen control hit area', '.rd-chart-expand { width:38px; height:38px;'],
  ['fullscreen control svg size', '.rd-chart-expand svg { width:22px; height:22px;'],
  ['fullscreen control rounded strokes', 'stroke-linecap:round; stroke-linejoin:round;'],
  ['fullscreen control keyboard focus', '.rd-chart-expand:focus-visible { outline:2px solid #8b93ea;'],
  ['fullscreen expand arrows use the right diagonal', "M14 10l6-6m0 0v5m0-5h-5M10 14l-6 6"],
  ['fullscreen collapse arrows use the right diagonal', "M20 4l-6 6m0 0V5m0 5h5M4 20l6-6"],
  ['fullscreen laptop bounds', 'width:min(1902px,calc(100vw - 48px))'],
  ['fullscreen chart uses measured dimensions', 'Math.round(container.clientWidth || 640)'],
  ['curves stay between adjacent values', 'Math.max(minimumY,Math.min(maximumY'],
  ['chart exposes zero boundary', 'data-zero-y="${top+plotHeight}"'],
  ['hidden metrics are faded', '.rd-metric-toggle[aria-pressed="false"] { opacity:.38; }'],
  ['refresh icon has no surface', '#rd-refresh { border:0; color:#5965d8; background:transparent;'],
  ['compact shared controls', '.rd-controls { width:min(100%,572px); display:grid; grid-template-columns:267px 267px 22px;'],
  ['global export control', 'id="rd-export-all" aria-label="Скачать все показатели в CSV"'],
  ['global export respects team selection', 'function selectedExportTeams()'],
  ['global export contains period and employee rows', "createCsvDownload([header,...periodRows,...peopleRows]"],
  ['select arrow has an inset', 'background-position:right 12px center'],
  ['analytics signal help', 'aria-label="Как работает персональный сигнал"'],
  ['analytics auto-save', "input.addEventListener('blur',saveAnalyticsRule)"],
  ['inactive days preference', 'id="rd-hide-inactive-days" type="checkbox" checked'],
  ['inactive days default to hidden', 'savedAnalyticsRule.hideInactiveDays !== false'],
  ['inactive day filtering stays local', 'function filterInactiveDays('],
  ['engagement legend controls', 'data-engagement-metric="approvals" aria-pressed="true"'],
  ['engagement metrics stay local', 'const visibleEngagementMetrics = new Set'],
  ['team totals aggregate employees', "sumSeries(team.people,team.name)"],
  ['separate dashboard tab', 'data-view="overview" aria-selected="true">Дашборд</button>'],
  ['review tab remains separate', 'data-view="dashboard" aria-selected="false">Ревью</button>'],
  ['dashboard combined chart', 'id="rd-dashboard-overall-chart"'],
  ['dashboard period bars', 'id="rd-dashboard-periods"'],
  ['dashboard legends live in panel headers', '<strong class="rd-widget-title">Все показатели</strong><div class="rd-metric-legend rd-dashboard-legend"'],
  ['mixed small-MR scale', 'const fixed=[0,1,3,5,7,10]'],
  ['period totals are visible', 'class="rd-dashboard-period-total"'],
  ['period metrics use engagement-style segments', 'class="rd-dashboard-period-metric"'],
  ['period total means changed lines', 'Добавлено + удалено'],
  ['period bars have interactive details', 'data-period-index="${rowIndex}"'],
  ['saved token is visibly masked', "const savedTokenMask = '••••••••••••'"],
  ['export explains current filter', 'data-tooltip="Скачать показатели с учётом выбранной команды"'],
  ['dashboard legends align right', '.rd-dashboard-legend { justify-content:flex-end; padding:0; }'],
  ['dashboard four metric cards', 'id="rd-dashboard-cards"'],
  ['dashboard breakdown title', '<strong>Расшифровка</strong>'],
  ['dashboard linear view', 'data-dashboard-view="line"'],
  ['dashboard horizontal-bar view', 'data-dashboard-view="bar"'],
  ['period-dependent groupings', 'function dashboardAvailableGroups()'],
  ['two-week preview window', "volumeRange.value==='week'?7:14"],
  ['zero periods are removed', 'function removeZeroDashboardPeriods('],
  ['dashboard uses existing loaded source', 'function dashboardSource()'],
  ['metric total inherits title typography', '.rd-dashboard-card-total { color:inherit; font:inherit; }'],
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
assert.ok(!page.includes('Реальные значения на единой нелинейной шкале'), 'unrequested chart captions must stay absent');
assert.ok(!page.includes('Количество, реальная нелинейная шкала'), 'unrequested axis captions must stay absent');
assert.ok(!page.includes('text-decoration:line-through'), 'hidden metric labels must not be struck through');
assert.ok(!page.includes('Ревью команды'), 'review heading must stay concise');
assert.ok(!page.includes('id="rd-save-analytics"'), 'analytics must save without a separate button');
assert.match(page,/\.rd-chart-expand \{[^}]*border:0;/, 'fullscreen icon must not have a border');
assert.match(page,/\.rd-icon-action \{[^}]*border:0;/, 'route icon actions must not have a border');
assert.ok(!page.includes('preserveAspectRatio="none"'), 'chart labels must not stretch with the SVG');

console.log(`UI checks passed: ${contracts.length + 13}`);
