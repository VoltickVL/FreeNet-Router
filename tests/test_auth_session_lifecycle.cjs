'use strict';
// Execute canonical login functions without router access or real credentials.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const html = fs.readFileSync(path.join(__dirname, '../freenet-ui/web/index.html'), 'utf8');
const start = html.indexOf('let authStatusGeneration=');
const end = html.indexOf('\nasync function logout(', start);
assert.ok(start >= 0 && end > start, 'canonical auth lifecycle is missing');
assert.match(html, /id="authRemember"[^>]*type="checkbox"[^>]*checked/, 'remember choice must exist before enhancement scripts');
assert.match(html, /Оставаться в системе 30 дней/);
const render = html.match(/^function renderAuth\(s\)\{[^\n]+/m)[0];
const reply = (status, data) => ({ok:status >= 200 && status < 300, status, json:async()=>data});
function environment(authenticated = true) {
  const ids = ['authSection','controlCenter','authConfirmField','authTitle','authSubtitle',
    'authSubmitBtn','authPassword','authPasswordConfirm','authRemember'];
  const nodes = Object.fromEntries(ids.map(id => [id,{hidden:false,value:'',textContent:'',disabled:false,checked:true}]));
  nodes.authPassword.value = 'fixture password not a real credential';
  nodes.authSection.hidden = authenticated;
  nodes.controlCenter.hidden = !authenticated;
  const calls = [], pending = [], notices = [], events = [];
  const context = vm.createContext({console,authConfigured:true,authAuthenticated:authenticated,
    el:id=>nodes[id],startControlCenter(){},stopControlCenter(){},hideBox(){},
    showBox:(...args)=>notices.push(args),
    document:{dispatchEvent:event=>events.push(event)},
    CustomEvent:class {constructor(type, init){this.type=type;this.detail=init.detail;}},
    fetch:async(url, init)=>{calls.push({url,init});const next=pending.shift();
      if (typeof next === 'function') return next();
      if (next instanceof Error) throw next;
      return next || reply(200,{configured:true,authenticated:true});}
  });
  vm.runInContext(render+'\n'+html.slice(start,end), context);
  return {context,nodes,calls,pending,notices,events};
}
(async()=>{
  // Only an explicit valid authenticated=false proves expiration.
  for (const failure of [reply(503,{error:'unavailable'}),reply(200,{error:'proxy'}),new Error('offline')]) {
    const x=environment();x.pending.push(failure);await x.context.loadAuthStatus();
    assert.equal(x.context.authAuthenticated,true);
    assert.equal(x.nodes.authSection.hidden,true);
    assert.equal(x.nodes.controlCenter.hidden,false);
    assert.equal(x.events.length,0,'uncertain response must not dispatch logout');
    assert.equal(x.calls[0].init.credentials,'include');
  }
  {
    const x=environment();x.pending.push(reply(200,{configured:true,authenticated:false}));
    await x.context.loadAuthStatus();assert.equal(x.context.authAuthenticated,false);
    assert.equal(x.nodes.controlCenter.hidden,true);
  }
  {
    const x=environment(false);x.pending.push(new Error('offline'));
    await x.context.loadAuthStatus();assert.equal(x.context.authAuthenticated,false);
    assert.equal(x.nodes.controlCenter.hidden,true,'offline cold start must not authorize');
  }
  // Explicit remember works without a late-loading fetch wrapper.
  for (const remembered of [true,false]) {
    const x=environment(false);x.nodes.authRemember.checked=remembered;
    await x.context.submitAuth();assert.equal(x.calls.length,1);
    assert.equal(x.calls[0].url,'/api/auth/login');
    assert.equal(JSON.parse(x.calls[0].init.body).remember,remembered);
    assert.equal(x.calls[0].init.credentials,'include');
    assert.equal(x.context.authAuthenticated,true);
    assert.equal(x.nodes.authPassword.value,'');
    assert.equal(x.nodes.authSubmitBtn.disabled,false);
  }
  {
    const x=environment(false);x.context.authConfigured=false;
    x.nodes.authPasswordConfirm.value=x.nodes.authPassword.value;
    await x.context.submitAuth();assert.equal(x.calls[0].url,'/api/auth/setup');
    assert.equal(JSON.parse(x.calls[0].init.body).remember,true);
  }
  {
    const x=environment(false);let finish;
    x.pending.push(()=>new Promise(resolve=>{finish=resolve;}));
    const first=x.context.submitAuth();await Promise.resolve();
    await x.context.submitAuth();assert.equal(x.calls.length,1,'double click creates one session');
    assert.equal(x.nodes.authSubmitBtn.disabled,true);
    finish(reply(200,{configured:true,authenticated:true}));await first;
    assert.equal(x.nodes.authSubmitBtn.disabled,false);
  }
  {
    const x=environment(false);let finish;
    x.pending.push(()=>new Promise(resolve=>{finish=resolve;}));
    const obsolete=x.context.loadAuthStatus();await Promise.resolve();
    await x.context.submitAuth();assert.equal(x.context.authAuthenticated,true);
    finish(reply(200,{configured:true,authenticated:false}));await obsolete;
    assert.equal(x.context.authAuthenticated,true,'pre-login response must not undo fresh login');
  }
  {
    const x=environment();let finish;
    x.pending.push(()=>new Promise(resolve=>{finish=resolve;}));
    const obsolete=x.context.loadAuthStatus();await Promise.resolve();
    await x.context.loadAuthStatus();
    finish(reply(200,{configured:true,authenticated:false}));await obsolete;
    assert.equal(x.context.authAuthenticated,true,'older status must not override newer status');
  }
  {
    const x=environment(false);x.pending.push(reply(401,{error:'invalid credentials'}));
    await x.context.submitAuth();assert.equal(x.context.authAuthenticated,false);
    assert.equal(x.nodes.controlCenter.hidden,true);
    assert.equal(x.nodes.authSubmitBtn.disabled,false);
  }
  console.log('PASS: canonical 30-day choice, opt-out, transient errors, stale responses and single-submit');
})().catch(error=>{console.error(error);process.exitCode=1;});
