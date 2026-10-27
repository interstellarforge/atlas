/* Design-only HTML fixtures; not application behavior. Keep existing Svelte logic.
   UI labels: demo/en.json; real app must continue using the existing en.json.
   Mock content below is seed/test data, not UI copy or implementation logic. */
const qs = new URLSearchParams(location.search);
const view = qs.get('view') || 'board';
const theme = qs.get('theme') === 'light' ? 'light' : 'dark';
document.documentElement.dataset.theme = theme;
const L = await (await fetch('./en.json')).json();
const app = document.querySelector('#app');
const samples = [
  {key:'ATL-142',type:'Story',title:'Build realtime board sync',status:'Backlog',priority:'High',due:'Oct 30',labels:['SSE','Platform'],assignee:'KM'},
  {key:'ATL-167',type:'Task',title:'Implement robust MCP batches',status:'Backlog',priority:'Medium',due:'Nov 2',labels:['MCP','API'],assignee:'JR',blocked:true},
  {key:'ATL-182',type:'Bug',title:'Fix mobile filter layout',status:'To do',priority:'Urgent',due:'Yesterday',labels:['Mobile','Bug'],assignee:'KM',late:true},
  {key:'ATL-193',type:'Story',title:'Work-item hierarchy and paths',status:'To do',priority:'High',due:'Nov 3',labels:['Core','SQLite'],assignee:'AT'},
  {key:'ATL-207',type:'Task',title:'Polish token-efficient responses',status:'In progress',priority:'High',due:'Nov 5',labels:['MCP','Tokens'],assignee:'JR'},
  {key:'ATL-209',type:'Story',title:'Project-based workflows',status:'In progress',priority:'Medium',due:'Nov 7',labels:['Workflow'],assignee:'KM'},
  {key:'ATL-215',type:'Task',title:'Review keyboard interactions',status:'Review',priority:'Medium',due:'Nov 8',labels:['UX','QA'],assignee:'AT'},
  {key:'ATL-230',type:'Task',title:'Add optimistic locking',status:'Done',priority:'Low',due:'Oct 24',labels:['API'],assignee:'KM'},
  {key:'ATL-231',type:'Task',title:'Document migration strategy',status:'Done',priority:'Low',due:'Oct 21',labels:['Docs'],assignee:'JR',archived:true}
];
const esc = (t) => String(t ?? '').replace(/[&<>"']/g, c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const attr = (t)=>esc(t);
const chip=(text,kind='')=>`<span class="atlas-chip ${kind?'atlas-chip--'+kind:''}">${esc(text)}</span>`;
const button=(text,kind='secondary',extra='')=>`<button type="button" class="atlas-btn atlas-btn--${kind}" ${extra}>${text}</button>`;
const logo=`<img class="atlas-brand-mark" src="../assets/atlas-mark.svg" alt=""/><span>ATLAS</span>`;
const nav=[['board',L.project.board],['list',L.project.list],['backlog',L.project.backlog],['hierarchy',L.project.hierarchy]];
const url=(v)=>`?view=${encodeURIComponent(v)}&theme=${theme}`;
const themeUrl=()=>`?view=${encodeURIComponent(view)}&theme=${theme==='dark'?'light':'dark'}`;
function shell(content,selected='projects') {
  return `<div class="atlas-shell">
  <header class="atlas-topbar">
   <a class="atlas-brand" href="${url('board')}">${logo}</a>
   <nav class="atlas-topnav" aria-label="Primary"><a href="${url('projects')}" ${selected==='projects'?'aria-current="page"':''}>${L.nav.projects}</a><a href="${url('list')}">${L.nav.myItems}</a><a href="${url('tokens')}">${L.nav.tokens}</a></nav>
   <div class="atlas-topbar-end"><span class="atlas-live">${L.states.connected}</span><a class="atlas-btn atlas-btn--secondary atlas-icon-btn" aria-label="${L.nav.theme}" href="${themeUrl()}">${theme==='dark'?'☼':'☾'}</a><details class="atlas-overflow-menu"><summary aria-label="More navigation">⋯</summary><div class="atlas-overflow-list"><a href="${url('tokens')}">${L.nav.tokens}</a><button>${L.nav.logout}</button></div></details><span class="atlas-avatar" aria-label="User initials">KM</span></div>
  </header>${content}</div>`;
}
function hero(title,subtitle,context='PROJECTS') {
 return `<section class="atlas-hero"><div><div class="atlas-eyebrow">${context} / ATLAS CORE</div><h1>${title}</h1><p>${subtitle}</p></div><div>${button('+ '+L.filter.add,'secondary')}</div></section>`;
}
function tabs(active) {return `<nav class="atlas-tabs" aria-label="Project views">${nav.map(([key,label])=>`<a class="atlas-tab" ${active===key?'aria-current="page"':''} href="${url(key)}">${label}</a>`).join('')}</nav>`;}
function filters() {
 const fields=`<div class="atlas-filters"><input class="atlas-input" aria-label="${L.filter.search}" placeholder="${L.filter.search}"/><select class="atlas-select" aria-label="${L.filter.type}"><option>${L.filter.type}</option></select><select class="atlas-select" aria-label="${L.filter.priority}"><option>${L.filter.priority}</option></select><select class="atlas-select" aria-label="${L.filter.assignee}"><option>${L.filter.assignee}</option></select><select class="atlas-select" aria-label="${L.filter.label}"><option>${L.filter.label}</option></select>${button(L.filter.archived)}${button(L.filter.clear,'link')}</div>`;
 return `<div class="atlas-desktop-filters">${fields}</div><details class="atlas-mobile-filters"><summary>${L.filter.mobile}</summary>${fields.replace('class="atlas-filters"','class="atlas-filters"')}</details>`;
}
function pageHeader(active,title,subtitle){return `<main class="atlas-page">${hero(title,subtitle)}<div class="atlas-project-head">${tabs(active)}<div class="atlas-muted" style="font-size:12px">SSE ● &nbsp; 4 views</div></div>${filters()}`;}
const typeKind=t=>t==='Story'?'type':t==='Bug'?'danger':'info';
const priorKind=t=>['Urgent','High'].includes(t)?'danger':t==='Medium'?'warn':'success';
const statusKind=t=>t==='Done'?'success':t==='In progress'?'warn':t==='Review'?'type':'info';
function card(item,variant='') {
 const cl=['atlas-card',item.blocked?'atlas-card--blocked':'',item.archived?'atlas-card--archived':'',variant?`atlas-card--${variant}`:''].join(' ');
 return `<article class="${cl}" tabindex="0" aria-label="${attr(item.key+' '+item.title)}"><div class="atlas-card-top"><span class="atlas-item-key">${item.key}</span>${chip(item.type,typeKind(item.type))}<span class="atlas-card-actions"><button class="atlas-card-action atlas-drag-handle" aria-label="${L.board.drag}" title="${L.board.drag}">⠿</button><button class="atlas-card-action" aria-label="${L.board.archive}" title="${L.board.archive}">⋯</button></span></div><div class="atlas-card-title" title="${attr(item.title)}">${esc(item.title)}</div><div class="atlas-card-bottom">${item.labels.slice(0,2).map(v=>chip(v)).join('')}<span class="atlas-card-spacer"></span>${item.priority==='Urgent'?'<span style="color:var(--atlas-danger);font-size:13px">↑</span>':''}<span class="atlas-due ${item.late?'atlas-due--late':''}">${item.due}</span><span class="atlas-avatar">${item.assignee}</span></div></article>`;
}
function boardColumn(title,items,code,wip=5) {
 const over=items.length>wip;
 return `<section class="atlas-column atlas-column--${code} ${over?'atlas-column--over-wip':''}"><header class="atlas-column-head"><span class="atlas-column-name"><i class="atlas-column-indicator"></i>${title}</span><span class="atlas-column-count">${items.length}</span><span class="atlas-wip">${wip?`${L.board.wip} ${items.length}/${wip}`:'No limit'}</span></header><div class="atlas-card-list">${items.length?items.map(i=>card(i)).join(''):`<div class="atlas-empty">${L.board.empty}<small>${L.board.emptyHint}</small></div>`}</div><button type="button" class="atlas-btn atlas-btn--secondary atlas-load-more">${L.board.loadMore}</button></section>`;
}
function board() {
 const cols=[['Backlog','backlog',samples.filter(i=>i.status==='Backlog'),1],['To do','todo',samples.filter(i=>i.status==='To do'),4],['In progress','progress',samples.filter(i=>i.status==='In progress'),3],['Review','review',samples.filter(i=>i.status==='Review'),3],['Done','done',samples.filter(i=>i.status==='Done'),0]];
 const markup=pageHeader('board',L.project.name,L.project.subtitle)+`<div class="atlas-views-caption">${L.project.board} · 9 work items · Drag by handle · Live updates</div><div class="atlas-board-scroll"><div class="atlas-board">${cols.map(([n,c,items,w])=>boardColumn(n,items,c,w)).join('')}</div></div></main>`;
 return shell(markup);
}
function cardShowcase() {
 const s=samples[0]; const src=samples[2];
 let states=[['Default',card(s)],['Keyboard focus',card(s).replace('tabindex="0"','tabindex="0" style="outline:3px solid var(--atlas-focus);outline-offset:3px"')],['Blocked',card(samples[1])],['Overdue',card(src)],['Archived',card(samples[8])],['Dragging: source',card(samples[3],'drag-source')],['Dragging: preview',card(samples[4],'drag-preview')],['Drop position',`<div class="atlas-drop-indicator"></div>${card(samples[4])}`]];
 return shell(`<main class="atlas-page">${hero('Card states','One fixed 88px card layout in every state.','COMPONENT SYSTEM')}<div class="atlas-viewbar"><h2>Card component reference</h2>${chip('Height: 88px','info')}</div><div class="atlas-card-showcase">${states.map(([name,content])=>`<section class="atlas-showcase-tile"><div class="atlas-showcase-label">${name}</div>${content}</section>`).join('')}</div></main>`);
}
function list() {
 let headers=['Key','Type','Title ↑','Status','Priority','Due','Assignee'];
 let rows=samples.map(i=>`<div class="atlas-table-row ${i.blocked?'atlas-table-row--blocked':''} ${i.archived?'atlas-table-row--archived':''}"><span class="atlas-item-key">${i.key}</span><div class="atlas-list-hide-mobile">${chip(i.type,typeKind(i.type))}</div><div class="atlas-cell-truncate">${i.blocked?'⛔ ':''}${esc(i.title)}</div><div>${chip(i.status,statusKind(i.status))}</div><div class="atlas-list-hide-mobile">${chip(i.priority,priorKind(i.priority))}</div><div class="atlas-list-hide-mobile ${i.late?'atlas-due--late':''}">${i.due}</div><div class="atlas-list-hide-mobile"><span class="atlas-avatar">${i.assignee}</span></div></div>`).join('');
 return shell(pageHeader('list','Project list','A fast, sortable view of the same work items.')+`<div class="atlas-table-wrap"><div class="atlas-table-head">${headers.map((h,j)=>`<div class="${[1,4,5,6].includes(j)?'atlas-list-hide-mobile':''} ${j===2?'atlas-sort-active':''}">${h}</div>`).join('')}</div>${rows}</div><div style="margin-top:12px">${button(L.board.loadMore)}</div></main>`);
}
function backlog() {
 const sections=[['EPIC · Realtime collaboration',samples.slice(0,3)],['EPIC · Agent integration',samples.slice(3,6)],['EPIC · Product quality',samples.slice(6)]];
 return shell(pageHeader('backlog','Project backlog','Group by epic and prioritize planned work.')+`<div class="atlas-collection">${sections.map(([g,arr])=>`<div class="atlas-group-head">⌄ &nbsp; ${g} <span style="margin-left:auto">${chip(String(arr.length),'type')}</span></div>${arr.map(i=>`<div class="atlas-backlog-row"><span class="atlas-item-key">${i.key}</span>${chip(i.type,typeKind(i.type))}<span class="atlas-row-title">${i.title}</span>${chip(i.status,statusKind(i.status))}</div>`).join('')}`).join('')}</div></main>`);
}
function hierarchy() {
 const tree=[{key:'ATL-100',title:'Platform foundation',type:'Epic',depth:0,status:'In progress'},{key:'ATL-140',title:'Realtime collaboration',type:'Story',depth:1,status:'In progress'},{...samples[0],depth:2},{...samples[1],depth:2},{key:'ATL-160',title:'MCP and AI integration',type:'Story',depth:1,status:'To do'},{...samples[4],depth:2},{key:'ATL-200',title:'Product experience',type:'Epic',depth:0,status:'Review'},{...samples[2],depth:1},{...samples[3],depth:1}];
 return shell(pageHeader('hierarchy','Work hierarchy','Navigate from epic to story to task.')+`<div class="atlas-collection">${tree.map(i=>`<div class="atlas-tree-row" data-depth="${i.depth}"><button class="atlas-tree-toggle" aria-label="Expand or collapse">${i.type==='Task'?'·':'⌄'}</button><span class="atlas-item-key">${i.key}</span>${chip(i.type,typeKind(i.type))}<span class="atlas-row-title">${i.title}</span>${chip(i.status,statusKind(i.status))}</div>`).join('')}</div></main>`);
}
const detailItem=samples[1];
function detail(){
 const body=shell(pageHeader('board','Atlas Core','Select a work item to inspect its details.')+`<div class="atlas-board-scroll"><div class="atlas-board">${boardColumn('Backlog',samples.slice(0,2),'backlog')}${boardColumn('To do',samples.slice(2,4),'todo')}${boardColumn('In progress',samples.slice(4,6),'progress')}</div></div></main>`);
 return body+`<div class="atlas-backdrop"></div><aside class="atlas-detail" role="dialog" aria-modal="true" aria-label="${L.detail.edit}"><div class="atlas-detail-header"><div class="atlas-detail-header-top"><div style="display:flex;align-items:center;gap:8px">${chip(detailItem.key,'info')}${chip(detailItem.type,'type')}${chip(L.board.blocked,'danger')}</div><button class="atlas-btn atlas-btn--secondary atlas-icon-btn" aria-label="${L.detail.close}">×</button></div><h2 style="margin-top:12px">${L.detail.edit}</h2></div>
 <div class="atlas-detail-content"><div class="atlas-banner atlas-banner--conflict">${L.states.elsewhere}</div>
 <div class="atlas-panel-section"><label class="atlas-field"><span class="atlas-field-label">${L.detail.title}</span><input class="atlas-input" value="${attr(detailItem.title)}"/></label><label class="atlas-field"><span class="atlas-field-label">${L.detail.description}</span><textarea class="atlas-textarea">Implement safe, idempotent batching through MCP. Return compact errors and avoid duplicate tasks on retries.</textarea></label></div>
 <div class="atlas-panel-section"><div class="atlas-panel-title">${L.detail.details}</div><div class="atlas-detail-fields">${[['Status','To do'],['Priority','High'],['Assignee','KM'],['Due date','2026-11-02'],['Labels','MCP · API'],['Parent','ATL-160']].map(([a,b])=>`<label class="atlas-field"><span class="atlas-field-label">${a}</span><input class="atlas-input" value="${b}"/></label>`).join('')}</div></div>
 <div class="atlas-panel-section"><div class="atlas-panel-title">${L.detail.customFields}</div><label class="atlas-field"><span class="atlas-field-label">Estimate (points)</span><input class="atlas-input" value="5"/></label></div>
 <div class="atlas-panel-section"><div class="atlas-panel-title">${L.detail.links}</div><div class="atlas-link-row"><span class="atlas-muted">Blocked by</span><span class="atlas-item-key">ATL-140</span><span class="atlas-row-title">Architecture review</span><button class="atlas-btn atlas-btn--link" aria-label="Remove link">×</button></div><div style="display:flex;gap:8px"><select class="atlas-select" aria-label="Link direction"><option>Relates to</option></select><input class="atlas-input" placeholder="ATL-…"/></div>${button('+ '+L.detail.addLink)}</div>
 </div><footer class="atlas-detail-footer">${button(L.detail.archive,'link')}${button(L.detail.discard)}${button(L.detail.save,'primary')}</footer></aside>`;
}
function login(){
 return `<main class="atlas-auth"><section class="atlas-auth-card"><div class="atlas-auth-header"><img src="../assets/atlas-mark.svg" width="68" height="68" alt="Atlas"/><div style="font-weight:800;letter-spacing:.19em;font-size:19px">ATLAS</div><h1>${L.auth.heading}</h1><p class="atlas-muted">${L.auth.subtitle}</p></div><form class="atlas-auth-form" onsubmit="event.preventDefault()"><label class="atlas-field"><span class="atlas-field-label">${L.auth.email}</span><input class="atlas-input" type="email" placeholder="you@example.com"/></label><label class="atlas-field"><span class="atlas-field-label">${L.auth.password}</span><input class="atlas-input" type="password" value="not-a-password"/></label><div style="display:flex;align-items:center;justify-content:space-between;gap:8px;font-size:12px"><label><input class="atlas-checkbox" type="checkbox" checked/> ${L.auth.remember}</label><a href="#">${L.auth.forgot}</a></div>${button(L.auth.login,'primary','type="submit"')}</form><div class="atlas-muted" style="text-align:center;font-size:12px">${L.auth.footer}</div></section></main>`;
}
function projects(){return shell(`<main class="atlas-page">${hero(L.nav.projects,'Every mission, one clear overview.','WORKSPACE')}<div class="atlas-viewbar"><h2>Active projects</h2>${button('+ '+L.project.newProject,'primary')}</div><div class="atlas-project-grid">${[['Atlas Core','Platform foundations','ATL'],['Exosphere','Family app and kiosk','EXO'],['Eclipse','Video editor','ECL']].map(([name,sub,key])=>`<section class="atlas-project-card"><div class="atlas-project-card-top">${chip(key,'info')}${chip('Active','success')}</div><div><h3>${name}</h3><p class="atlas-muted">${sub}</p></div><div class="atlas-project-card-bottom"><span class="atlas-muted">Updated recently</span><span class="atlas-avatar">KM</span></div></section>`).join('')}</div><div style="margin-top:26px" class="atlas-empty"><h3>${L.states.emptyTitle}</h3>${L.states.emptyBody}</div></main>`);}
function tokens(){return shell(`<main class="atlas-page">${hero(L.nav.tokens,'Grant scoped access to clients and AI agents.','SECURITY')}<div class="atlas-empty"><h2>${L.states.tokenEmpty}</h2><p>Connect an agent with a scoped token.</p>${button('Create token','primary')}</div></main>`);}
function notfound(){return shell(`<main class="atlas-page"><div class="atlas-notfound"><h1>404</h1><h2>${L.states.notFound}</h2>${button(L.nav.projects)}</div></main>`);}

function newproject(){
 return shell(`<main class="atlas-page">${hero(L.project.newProject,'Create a new workspace project.','PROJECTS')}<form class="atlas-project-form" onsubmit="event.preventDefault()"><label class="atlas-field atlas-field--full"><span class="atlas-field-label">Name</span><input class="atlas-input" placeholder="Project name"/></label><label class="atlas-field"><span class="atlas-field-label">Project key</span><input class="atlas-input" placeholder="ATL"/></label><label class="atlas-field"><span class="atlas-field-label">Visibility</span><select class="atlas-select"><option>Private</option></select></label><label class="atlas-field atlas-field--full"><span class="atlas-field-label">Description</span><textarea class="atlas-textarea"></textarea></label>${button('Cancel')}${button('Create project','primary')}</form></main>`);
}
function emptyprojects(){
 return shell(`<main class="atlas-page">${hero(L.nav.projects,'Everything starts with one project.','WORKSPACE')}<div class="atlas-empty"><h2>${L.states.emptyTitle}</h2><p>${L.states.emptyBody}</p><a class="atlas-btn atlas-btn--primary" href="${url('newproject')}">${L.project.newProject}</a></div></main>`);
}
function crossboard(){
 let groups=[['To do',samples.slice(0,2)],[ 'Active',samples.slice(2,5)],[ 'Done',samples.slice(6,9)]];
 return shell(`<main class="atlas-page">${hero(L.project.crossProject,'Across projects · three shared status categories.','OVERVIEW')}<div class="atlas-viewbar"><h2>All work</h2>${chip(L.project.readOnly,'info')}</div><div class="atlas-cross-board">${groups.map(([name,items])=>`<section class="atlas-cross-column"><div class="atlas-column-head"><strong>${name}</strong><span class="atlas-column-count">${items.length}</span></div><div class="atlas-card-list">${items.map(i=>card(i)).join('')}</div></section>`).join('')}</div></main>`);
}
app.innerHTML = ({board,card:cardShowcase,list,detail,login,backlog,hierarchy,projects,tokens,notfound,newproject,emptyprojects,crossboard}[view]||notfound)();
