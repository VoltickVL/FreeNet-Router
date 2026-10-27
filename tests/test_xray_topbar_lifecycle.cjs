'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../freenet-ui/web/xray-core-manager.js'), 'utf8');
const html = fs.readFileSync(path.join(__dirname, '../freenet-ui/web/index.html'), 'utf8');
const renderAuth = html.match(/function renderAuth\(s\)\{[^\n]+/)[0];
assert.ok(renderAuth.includes("freenet:auth-changed"), 'canonical login must notify read-only consumers');

function environment({authenticated = false, shell = true, replies = [], xkeenReplies = []} = {}) {
  const nodes = new Map(), listeners = new Map(), windowListeners = new Map();
  const timers = new Map(), calls = [], xkeenCalls = [], observers = [];
  let now = 0, serial = 1;
  class Element {
    constructor(tag = 'div') { this.tagName = tag; this.children = []; this.hidden = false; this.attributes = {}; this.style = {}; this.textContent = ''; this.parentNode = null; this._id = ''; this.events = new Map(); this.classList = {toggle() {}, add() {}, remove() {}}; }
    set id(value) { this._id = value; nodes.set('#' + value, this); }
    get id() { return this._id; }
    set innerHTML(value) {
      this.markup = value;
      if (value.includes('id="xrayTopbarVersion"')) {
        const child = new Element('strong'); child.id = 'xrayTopbarVersion'; child.textContent = 'версия…'; this.appendChild(child);
      }
      if (value.includes('id="xkeenTopbarVersion"')) {
        const child = new Element('strong'); child.id = 'xkeenTopbarVersion'; child.textContent = 'версия…'; this.appendChild(child);
      }
    }
    get firstChild() { return this.children[0] || null; }
    appendChild(child) { if (child.parentNode) child.parentNode.removeChild(child); this.children.push(child); child.parentNode = this; return child; }
    insertBefore(child, before) { this.appendChild(child); return child; }
    removeChild(child) { this.children = this.children.filter(x => x !== child); child.parentNode = null; return child; }
    setAttribute(k,v) { this.attributes[k] = String(v); }
    addEventListener(k, f) { this.events.set(k, f); }
    querySelector(selector) { return nodes.get(selector) || null; }
  }
  const head = new Element('head'), body = new Element('body');
  const dispatch = (map, event) => { for (const f of map.get(event.type) || []) f(event); };
  const add = (map, k, f) => map.set(k, [...(map.get(k) || []), f]);
  const doc = {head, body, readyState:'complete', visibilityState:'visible',
    querySelector: s => nodes.get(s) || null, createElement: tag => new Element(tag),
    addEventListener:(k,f) => add(listeners,k,f), dispatchEvent:e => dispatch(listeners,e)};
  const timeout = (fn, delay = 0) => { const id=serial++; timers.set(id,{due:now+delay,fn}); return id; };
  const context = {console, document:doc, authAuthenticated:authenticated, authConfigured:true,
    AbortController, CustomEvent:class {constructor(type, data = {}) {this.type=type;Object.assign(this,data);}},
    setTimeout:timeout, clearTimeout:id=>timers.delete(id), requestAnimationFrame:fn=>timeout(fn,0),
    MutationObserver:class {constructor(fn){observers.push(fn);}observe(){}},
    fetch: async (url, options={}) => {
      const installed = url === '/api/xkeen/installed';
      (installed ? xkeenCalls : calls).push({url,method:options.method || 'GET'});
      const next = (installed ? xkeenReplies : replies).shift();
      if (typeof next === 'function') return next(options.signal);
      if (next instanceof Error) throw next;
      const r = next || (installed
        ? {status:200,data:{success:true,mutation:'NONE',installed_version:'1.1.3.9',installed_channel:'stable'}}
        : {status:200,data:{success:true,online:true,version:'26.9.9'}});
      return {ok:r.status>=200 && r.status<300,status:r.status,json:async()=>r.data};
    },
    el:id=>nodes.get('#'+id), startControlCenter:()=>{}, stopControlCenter:()=>{}
  };
  context.window = {addEventListener:(k,f)=>add(windowListeners,k,f), FreeNetXrayControl:{}};
  for (const id of ['authSection','controlCenter','authConfirmField','authTitle','authSubtitle','authSubmitBtn','authPassword']) {const n=new Element();n.id=id;}
  nodes.get('#controlCenter').hidden=!authenticated;
  function mountShell() {const top=new Element();nodes.set('.topbar',top);nodes.set('.topbar.overview-approved',top);const facts=new Element();facts.id='overviewApprovedTop';top.appendChild(facts);body.appendChild(top);}
  if (shell) mountShell();
  vm.createContext(context);
  vm.runInContext(renderAuth, context);
  vm.runInContext(source, context);
  const flush = async()=>{for(let n=0;n<8;n++) await Promise.resolve();};
  async function tick(ms){await flush();const end=now+ms;for(let n=0;n<100;n++){const next=[...timers.entries()].filter(([,v])=>v.due<=end).sort((a,b)=>a[1].due-b[1].due)[0];if(!next){now=end;await flush();return;}const [id,task]=next;now=task.due;timers.delete(id);task.fn();await flush();}throw Error('unbounded timer loop');}
  return {context,calls,xkeenCalls,replies,xkeenReplies,timers,observers,nodes,flush,tick,
    text:()=>nodes.get('#xrayTopbarVersion')?.textContent,
    xkeenText:()=>nodes.get('#xkeenTopbarVersion')?.textContent,
    auth:state=>context.renderAuth({configured:true,authenticated:state}),
    remount:()=>{nodes.delete('#xrayTopbarChip');nodes.delete('#xrayTopbarVersion');for(const fn of observers)fn();},
    remountXKeen:()=>{nodes.delete('#xkeenTopbarChip');nodes.delete('#xkeenTopbarVersion');for(const fn of observers)fn();},
    mount:()=>{mountShell();for(const fn of observers)fn();},
    signal:type=>dispatch(windowListeners,{type}),
  };
}

(async()=>{
  const late=environment(); await late.tick(30000);
  assert.equal(late.calls.length,0,'unauthenticated page must not burn Xray read retries');
  assert.equal(late.xkeenCalls.length,0,'unauthenticated page must not fetch installed XKeen');
  late.auth(true);await late.flush();assert.equal(late.text(),'v26.9.9','late login must populate Xray without a click');
  assert.equal(late.xkeenText(),'v1.1.3.9','late login must populate XKeen without opening its manager');
  assert.equal(late.calls.length,1);
  assert.equal(late.xkeenCalls.length,1,'one installed-only XKeen read during login');
  const xrayIcon = late.nodes.get('#xrayTopbarChip').markup.match(/<svg[\s\S]*?<\/svg>/)?.[0];
  const xkeenIcon = late.nodes.get('#xkeenTopbarChip').markup.match(/<svg[\s\S]*?<\/svg>/)?.[0];
  assert.ok(xrayIcon && xkeenIcon && xrayIcon !== xkeenIcon,'Xray core and XKeen must have distinct recognizable icons');
  late.remount();await late.flush();assert.equal(late.text(),'v26.9.9','recreated chip must render the cached snapshot');
  assert.equal(late.calls.length,1,'remount should not query release catalog');
  late.remountXKeen();await late.flush();
  assert.equal(late.xkeenText(),'v1.1.3.9','recreated XKeen chip must show cached installed version');
  assert.equal(late.xkeenCalls.length,1,'XKeen remount must not refetch installed version');

  const retry=environment({authenticated:true,replies:[{status:503,data:{success:false}}, {status:200,data:{success:true,online:true,version:'26.10.1'}}]});
  await retry.tick(800);assert.equal(retry.text(),'v26.10.1');assert.equal(retry.calls.length,2);
  const fail=environment({authenticated:true,replies:Array.from({length:4},()=>({status:503,data:{success:false}}))});
  await fail.tick(30000);assert.equal(fail.calls.length,4,'bounded retry budget');assert.equal(fail.text(),'недоступно','never leave an eternal version placeholder');
  await fail.tick(30000);assert.equal(fail.calls.length,4);
  fail.signal('online');await fail.flush();assert.equal(fail.text(),'v26.9.9','network recovery renews read budget');

  const delayed=environment({authenticated:true,shell:false});await delayed.tick(30000);assert.equal(delayed.calls.length,0);
  delayed.mount();await delayed.flush();assert.equal(delayed.text(),'v26.9.9','late shell mount starts hydration');

  let resolveOld;
  const race=environment({authenticated:true,replies:[()=>new Promise(resolve=>{resolveOld=resolve;}),{status:200,data:{success:true,online:true,version:'26.10.2'}}]});
  await race.flush();race.auth(false);race.auth(true);await race.flush();
  resolveOld({ok:true,status:200,json:async()=>({success:true,online:true,version:'1.2.3'})});await race.flush();
  assert.equal(race.text(),'v26.10.2','previous-session response must not overwrite current session');
  assert.equal(race.calls.length,2);

  const unauthorized=environment({authenticated:true,replies:[{status:401,data:{error:'authentication required'}}]});
  await unauthorized.tick(30000);assert.equal(unauthorized.calls.length,1,'401 awaits real authentication, no timer storm');
  unauthorized.auth(true);await unauthorized.flush();assert.equal(unauthorized.text(),'v26.9.9');

  const hung=environment({authenticated:true,replies:[signal=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(Error('aborted')))), {status:200,data:{success:true,online:false,version:'26.9.9'}}]});
  await hung.tick(5800);assert.equal(hung.text(),'остановлен','hung request timeout must not strand hydration');
  assert.equal(hung.calls.length,2);
  const missing=environment({authenticated:true,replies:[{status:200,data:{success:true,online:true}}]});
  await missing.tick(800);assert.equal(missing.text(),'v26.9.9','missing version must be retried');

  for(const env of [late,retry,fail,delayed,race,unauthorized,hung,missing]) {
    assert.ok(env.calls.every(call=>call.method==='GET' && call.url==='/api/xray/service'),'hydration may not invoke mutations or release scans');
  }
  const installed = environment({authenticated:true,xkeenReplies:[
    {status:503,data:{success:false}},
    {status:200,data:{success:true,mutation:'NONE',installed_version:'2.1',installed_channel:'stable'}}
  ]});
  await installed.tick(800);
  assert.equal(installed.xkeenText(),'v2.1','transient XKeen error must retry and recover without a click');
  assert.equal(installed.xkeenCalls.length,2);

  const unverified = environment({authenticated:true,xkeenReplies:Array.from({length:4},()=>({status:503,data:{success:false}}))});
  await unverified.tick(30000);
  assert.equal(unverified.xkeenCalls.length,4,'XKeen retry budget must be bounded');
  assert.equal(unverified.xkeenText(),'недоступно','XKeen must not show an eternal version placeholder');
  await unverified.tick(30000);
  assert.equal(unverified.xkeenCalls.length,4);
  unverified.signal('online');await unverified.flush();
  assert.equal(unverified.xkeenText(),'v1.1.3.9','network recovery must renew XKeen budget');

  const missingKeen = environment({authenticated:true,shell:false});
  await missingKeen.tick(30000);
  assert.equal(missingKeen.xkeenCalls.length,0,'missing shell must not consume XKeen requests');
  missingKeen.mount();await missingKeen.flush();
  assert.equal(missingKeen.xkeenText(),'v1.1.3.9','late shell mount hydrates XKeen');

  let oldKeen;
  const keenRace = environment({authenticated:true,xkeenReplies:[
    () => new Promise(resolve=>{oldKeen=resolve;}),
    {status:200,data:{success:true,mutation:'NONE',installed_version:'2.3'}}
  ]});
  await keenRace.flush();
  keenRace.auth(false);keenRace.auth(true);await keenRace.flush();
  oldKeen({ok:true,status:200,json:async()=>({success:true,mutation:'NONE',installed_version:'1.0'})});
  await keenRace.flush();
  assert.equal(keenRace.xkeenText(),'v2.3','stale previous-session XKeen response must not overwrite current');
  assert.equal(keenRace.xkeenCalls.length,2);

  const keen401 = environment({authenticated:true,xkeenReplies:[{status:401,data:{success:false}}]});
  await keen401.tick(30000);
  assert.equal(keen401.xkeenCalls.length,1,'XKeen 401 must not retry until reauthentication');
  assert.equal(keen401.xkeenText(),'вход требуется');
  keen401.auth(true);await keen401.flush();
  assert.equal(keen401.xkeenText(),'v1.1.3.9');

  const keenHang = environment({authenticated:true,xkeenReplies:[
    signal=>new Promise((resolve,reject)=>signal.addEventListener('abort',()=>reject(Error('aborted')))),
    {status:200,data:{success:true,mutation:'NONE',installed_version:'1.1.3.9'}}
  ]});
  await keenHang.tick(6600);
  assert.equal(keenHang.xkeenText(),'v1.1.3.9','timed-out installed read must retry without changing runtime');

  for (const env of [late,retry,fail,delayed,race,unauthorized,hung,missing,installed,unverified,missingKeen,keenRace,keen401,keenHang]) {
    assert.ok(env.xkeenCalls.every(c=>c.url==='/api/xkeen/installed' && c.method==='GET'),
      'background XKeen lifecycle must never call update-check, plan, apply or Xray actions');
  }
  const background=source.slice(source.indexOf('  async function refreshTopbarVersion()'),source.indexOf('\n  function body()'));
  assert.ok(!background.includes('serviceMutationUnknown = false'));
  assert.ok(!background.includes('fetchServiceSnapshot()'));
  console.log('Xray/XKeen topbar: login, late shell, bounded retry, timeout, session race, remount, independent icons and read-only boundary PASS');
})().catch(error=>{console.error(error);process.exitCode=1;});
