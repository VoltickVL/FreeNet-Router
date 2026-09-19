// End-to-end presentation gate against REAL Go canonical HTML and shipped assets.
// Runtime APIs use synthetic fixtures; asset requests reach the production handlers.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const {spawn} = require('node:child_process');
const root = path.resolve(__dirname,'..');
const artifacts = path.join(root,'test-artifacts','control-center');
const temp = fs.mkdtempSync(path.join(os.tmpdir(),'freenet-production-browser-'));
const addressFile = path.join(temp,'address');
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms));
const server = spawn('go',['test','-run','^TestControlCenterBrowserServer$','-count=1','-timeout=5m'],{
  cwd:path.join(root,'freenet-ui'),env:{...process.env,FREENET_BROWSER_ADDRESS_FILE:addressFile},stdio:['ignore','pipe','pipe']
});
let serverLog=''; server.stdout.on('data',b=>{serverLog+=b}); server.stderr.on('data',b=>{serverLog+=b});
let serverExited=false;server.on('exit',()=>{serverExited=true});
const locations=[['be','Брюссель','Бельгия'],['de','Берлин','Германия'],['nl','Амстердам','Нидерланды'],['fi','Хельсинки','Финляндия']];
const profiles=Array.from({length:49},(_,i)=>{
  const [code,city,country]=locations[i%locations.length];
  return {id:`fixture-${i}`,name:`${code.toUpperCase()} ${city} ${i+1}, ${country}, Extra`,country_code:code,address:`192.0.2.${i+10}`,port:443};
});
const current=profiles[0], target=profiles[1];
const ep=p=>`${p.address}:${p.port}`;
let status={version:'0.3.92',country:'Бельгия',city:'Брюссель',country_code:'be',profile_label:current.name,endpoint:ep(current),xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'vladlink',isp_label:'Владлинк',setup_complete:true,install_scenario:'existing_stack',subscription_configured:true,busy:false,updater_busy:false};
let planMode='ok',planDelay=0,applyMode='ok';
const calls=[],unhandled=[],errors=[];
let browser,page;
const P='#fnVpnPickerV2Panel', T='#fnVpnPickerV2Toggle', S='#fnVpnPickerV2Search', R='#fnVpnPickerV2Results', F='#fnVpnPickerV2Footer', C='#fnVpnPickerV2Connect';
const countApply=()=>calls.filter(x=>x.path==='/api/network-profile/apply').length;
async function geometry(label){
  const data=await page.evaluate(({P,T,S,R,F,C})=>{
    const box=s=>{const n=document.querySelector(s),r=n.getBoundingClientRect();return {x:r.x,y:r.y,right:r.right,bottom:r.bottom,width:r.width,height:r.height,scrollWidth:n.scrollWidth,clientWidth:n.clientWidth,scrollHeight:n.scrollHeight,clientHeight:n.clientHeight}};
    return {panel:box(P),toggle:box(T),search:box(S),results:box(R),footer:box(F),connect:box(C),vw:innerWidth,vh:innerHeight,rootWidth:document.documentElement.scrollWidth,oldBackdrop:!!document.querySelector('#fnVpnPickerBackdrop')&&getComputedStyle(document.querySelector('#fnVpnPickerBackdrop')).display!=='none'};
  },{P,T,S,R,F,C});
  console.log('GEOMETRY '+label+' '+JSON.stringify(data));
  assert.ok(data.panel.x>=0&&data.panel.right<=data.vw+1&&data.panel.y>=0&&data.panel.bottom<=data.vh+1,label+' panel must fit viewport');
  assert.ok(data.footer.bottom<=data.panel.bottom+1&&data.footer.y>=data.results.bottom-1,label+' footer below list, not clipped');
  assert.ok(data.connect.bottom<=data.panel.bottom+1,label+' connect visible');
  assert.ok(data.search.width>=data.panel.width*.83,label+' full-width search');
  assert.ok(data.results.scrollWidth<=data.results.clientWidth+1,label+' no horizontal list scroll');
  assert.ok(data.panel.scrollWidth<=data.panel.clientWidth+1,label+' no horizontal panel scroll');
  assert.equal(data.oldBackdrop,false,label+' no old fullscreen backdrop');
  return data;
}
async function capture(label){
  fs.mkdirSync(artifacts,{recursive:true});
  await page.screenshot({path:path.join(artifacts,label+'.png')});
  if(process.env.FREENET_PREVIEW_LOGS==='yes'){
    const shot=await page.locator(P).screenshot({type:'jpeg',quality:38});
    console.log('FREENET_PREVIEW_'+label+'='+shot.toString('base64'));
  }
}
(async()=>{
  for(let i=0;i<600&&!fs.existsSync(addressFile);i++){if(serverExited)throw new Error('Go fixture failed: '+serverLog);await delay(100)}
  assert.ok(fs.existsSync(addressFile),'Go production fixture startup timed out: '+serverLog);
  const base=fs.readFileSync(addressFile,'utf8');
  browser=await chromium.launch({headless:true});
  page=await browser.newPage({viewport:{width:1440,height:1000}});
  page.setDefaultTimeout(12000);page.on('pageerror',e=>errors.push(e.message));
  const answer=(route,body,code=200)=>route.fulfill({status:code,contentType:'application/json',body:JSON.stringify(body)});
  await page.route('**/api/**',async route=>{
    const req=route.request(),url=new URL(req.url());
    if(url.pathname.includes('/assets/'))return route.continue();
    calls.push({path:url.pathname,method:req.method(),query:url.search,body:req.postData()});
    if(url.pathname==='/api/auth/status')return answer(route,{configured:true,authenticated:true});
    if(url.pathname==='/api/status')return answer(route,status);
    if(url.pathname==='/api/operation/state')return answer(route,{success:true,active:false});
    if(url.pathname==='/api/subscription')return answer(route,{success:true,configured:true});
    if(url.pathname==='/api/vpn/current-quality'&&url.searchParams.get('job')==='cache'){
      const p=profiles.find(p=>ep(p)===status.endpoint)||current;
      return answer(route,{success:true,available:true,scanned_at:new Date().toISOString(),candidates:[{...p,endpoint:status.endpoint,current:true,tested:true,eligible:true,available:true,reachable:true,download_mbps:55,application_rtt_ms:105,tcp_rtt_ms:70,jitter_ms:5,media_samples:4,media_stalls:0,service_ok:4,service_total:4}]});
    }
    if(url.pathname==='/api/network-profile/plan'){
      const id=url.searchParams.get('provider_profile_id');
      if(planMode==='offline')return answer(route,{success:false,error:'Fixture subscription unavailable'},503);
      if(id&&planDelay)await delay(planDelay);
      const selected=profiles.find(p=>p.id===id);
      const provider_plan=!id?undefined:planMode==='error'?{success:false,candidate_xray_valid:false,mutation:'NONE',error:'Конфигурация сервера не прошла проверку Xray. Текущий VPN не изменён.'}:{success:true,candidate_xray_valid:true,mutation:'NONE',endpoint:ep(selected||target)};
      return answer(route,{success:true,supported:true,active:true,provider_plan,extra_profiles:profiles});
    }
    if(url.pathname==='/api/network-profile/apply'){
      const body=JSON.parse(req.postData());
      assert.deepEqual(Object.keys(body).sort(),['confirm','operation','profile_id']);
      assert.equal(body.operation,'provider');assert.equal(body.confirm,true);
      const selected=profiles.find(p=>p.id===body.profile_id);assert.ok(selected);
      if(applyMode==='unknown')return answer(route,{success:false,error:'Результат операции не подтверждён',rollback_state:'UNKNOWN'},502);
      await delay(250);
      status={...status,country:'',city:'',country_code:'',profile_label:selected.name,endpoint:ep(selected)};
      return answer(route,{success:true,applied:true,rollback_state:'NOT_NEEDED'});
    }
    if(url.pathname==='/api/xray/service')return answer(route,{success:true,version:'v26.9.9',current_version:'v26.9.9',online:true});
    if(url.pathname==='/api/settings-v3')return answer(route,{success:true,auto_vpn:{enabled:true,country_scope:'region',countries:[]},automation:{success:true,current_profile:status.profile_label,current_endpoint:status.endpoint,country_code:status.country_code,settings:{enabled:true},events:[]},subscription:{enabled:false,interval:'6h'},geodata:{enabled:true,interval:'3h'},freenet:{enabled:false,interval:'12h'},backup:{enabled:false,interval:'24h'},events:[]});
    if(url.pathname==='/api/automation')return answer(route,{success:true,settings:{enabled:true,country_scope:'region',countries:[]},current_profile:status.profile_label,current_endpoint:status.endpoint,country_code:status.country_code,events:[]});
    if(url.pathname==='/api/settings-v3/countries')return answer(route,{success:true,countries:locations.map(([code,,name])=>({code,name,available:true})),selected:[],fresh:true});
    if(url.pathname==='/api/geodata/files')return answer(route,{success:true,files:[],search_enabled:false});
    if(url.pathname.startsWith('/api/system/update/'))return answer(route,{success:true,ready:true,state:'IDLE',current_version:'v0.3.92',latest_version:'v0.3.92',update_available:false,rollback_state:'NOT_NEEDED'});
    if(req.method()!=='GET')throw new Error('Unexpected mutation '+url.pathname);
    unhandled.push(url.pathname);
    return answer(route,{success:true,available:false,configured:true,active:false,events:[]});
  });
  await page.goto(base);
  await page.waitForFunction(()=>document.documentElement.dataset.freenetCanonicalReady==='1');
  await page.locator(T).waitFor({state:'visible'});
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Country')?.textContent==='Бельгия');
  assert.equal(await page.locator('#fnVpnPickerToggle').count(),0,'legacy picker is not another owner');
  assert.equal(await page.locator(T).count(),1,'exactly one current control');
  assert.equal(await page.locator(P).isHidden(),true,'closed by default');
  assert.equal(countApply(),0,'opening Control Center never applies VPN');
  assert.equal(calls.filter(c=>c.path.startsWith('/api/vpn/')&&!c.query.includes('job=cache')).length,0,'saved quality is read without starting a speed scan');
  assert.deepEqual(errors,[],'production script load must be error-free');
  status={...status,country:'',country_code:'',city:''};
  await page.evaluate(()=>loadStatus());
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Flag')?.dataset.country==='be');
  await page.waitForFunction(()=>document.querySelector('#bestCurrentFlag')?.classList.contains('flag-be'));
  assert.equal(await page.locator('#fnVpnPickerV2Country').textContent(),'Бельгия','current label identity handles missing country fields');
  const order=await page.locator('#overviewApprovedTop').evaluate(n=>Array.from(n.children).map(x=>x.matches('.fn-xray-topbar')?'xray':x.id==='fnVpnPickerV2Host'?'vpn':x.id==='topFreenetUpdate'?'freenet':/DNS/i.test(x.textContent||'')?'dns':'other').filter(x=>x!=='other'));
  assert.deepEqual(order,['xray','vpn','dns','freenet']);
  await page.locator(T).click();
  await page.locator(P).waitFor({state:'visible'});
  await page.waitForFunction(()=>document.querySelectorAll('#fnVpnPickerV2Results button').length===49);
  const initial=await geometry('desktop-initial');
  assert.ok(initial.panel.y>=initial.toggle.bottom,'desktop panel is anchored below VPN control');
  const flags=await page.evaluate(()=>{
    const a=document.querySelector('#fnVpnPickerV2Flag'),b=document.querySelector('#fnVpnPickerV2CurrentFlag'),c=document.querySelector('#bestCurrentFlag');
    return {source:a.dataset.flagSource,chip:getComputedStyle(a).backgroundImage,panel:getComputedStyle(b).backgroundImage,overview:getComputedStyle(c).backgroundImage,all:Array.from(document.querySelectorAll('#fnVpnPickerV2Results .fnv2-flag')).every(n=>n.dataset.flagSource==='canonical'),emoji:/[\u{1F1E6}-\u{1F1FF}]/u.test(document.querySelector('#fnVpnPickerV2Panel').textContent)};
  });
  assert.equal(flags.source,'canonical');assert.equal(flags.chip,flags.overview);assert.equal(flags.panel,flags.overview);assert.equal(flags.all,true);assert.equal(flags.emoji,false);
  await capture('desktop-list');
  for(const query of ['герм','Germany','DE']){
    await page.locator(S).fill(query);
    assert.ok(await page.locator(R+' button').count()>0,'bilingual search '+query);
    assert.equal(await page.locator(R+' .fnv2-flag:not([data-country="de"])').count(),0);
  }
  await page.locator(S).fill('nonexistent-fixture');assert.equal(await page.locator(R+' button').count(),0);assert.equal(await page.locator(R+' .fnv2-empty').isVisible(),true);
  await page.locator(S).fill('DE');planDelay=1300;
  await page.locator(R+' button[data-profile-id="fixture-1"]').click();
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='checking');
  assert.equal(await page.locator(C).isDisabled(),true);assert.equal(await page.locator(C).textContent(),'Подключиться');
  assert.equal(await page.locator('#fnVpnPickerV2Country').textContent(),'Бельгия','selection must not be shown as connected');
  await geometry('checking');
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='ready');
  await geometry('ready');await capture('desktop-ready');assert.equal(countApply(),0,'plan/check is read-only');
  await page.locator(C).click();await page.locator(C).dispatchEvent('click');
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Германия');
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='success');
  assert.equal(countApply(),1,'double click cannot create a second apply');assert.equal(await page.locator(C).isDisabled(),true);
  await page.locator('#fnVpnPickerV2Reset').click();planDelay=0;planMode='error';
  await page.locator(S).fill('NL');await page.locator(R+' button').first().click();
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='error');
  assert.equal(await page.locator(C).isDisabled(),true);assert.match(await page.locator('#fnVpnPickerV2Detail').textContent(),/Xray/);
  await geometry('validation-error');assert.equal(countApply(),1);
  await page.locator('#fnVpnPickerV2Reset').click();planMode='offline';await page.evaluate(()=>loadNetworkPlan());
  await page.locator(S).fill('');await page.waitForFunction(()=>document.querySelectorAll('#fnVpnPickerV2Results button').length===49);
  await page.waitForFunction(()=>!document.querySelector('#fnVpnPickerV2Stale').hidden);
  assert.equal(countApply(),1,'failed refresh preserves last good list without mutation');planMode='ok';
  await page.keyboard.press('Escape');
  for(const viewport of [{width:1440,height:900},{width:980,height:800},{width:760,height:700},{width:390,height:844},{width:844,height:390}]){
    await page.setViewportSize(viewport);await page.locator(T).click();await page.locator(P).waitFor({state:'visible'});await delay(100);
    await geometry(`${viewport.width}x${viewport.height}`);if(viewport.width===390)await capture('mobile-list');
    await page.keyboard.press('Escape');assert.equal(await page.locator(P).isHidden(),true);
  }
  await page.setViewportSize({width:1440,height:1000});
  await page.evaluate(()=>setPage('settings'));await delay(300);await page.locator(T).click();await geometry('settings-route');await page.keyboard.press('Escape');
  await page.evaluate(()=>setPage('overview'));
  for(let i=0;i<10;i++){await page.locator(T).click();await page.keyboard.press('Escape')}
  await page.evaluate(()=>new Promise(resolve=>{const n=document.createElement('div');document.body.appendChild(n);for(let i=0;i<100;i++)n.textContent=String(i);setTimeout(()=>{n.remove();resolve()},80)}));
  assert.equal(await page.locator(T).count(),1);assert.equal(await page.locator(P).count(),1);assert.equal(countApply(),1);
  status={...status,xray_online:false};await page.evaluate(()=>loadStatus());
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Не подключён');
  await page.locator(T).click();assert.equal(await page.locator('#fnVpnPickerV2State').getAttribute('data-online'),'false');
  status={...status,xray_online:true};await page.evaluate(()=>loadStatus());
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Германия');
  await page.locator(S).fill('FI');await page.locator(R+' button').first().click();
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='ready');
  applyMode='unknown';await page.locator(C).click();
  await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='error');
  assert.equal(await page.locator(C).isDisabled(),true);assert.equal(await page.locator('#fnVpnPickerV2Reset').isDisabled(),true);
  assert.equal(await page.locator(R+' button:not(:disabled)').count(),0,'unknown rollback is STOP');
  await page.locator(C).dispatchEvent('click');assert.equal(countApply(),2,'unknown result cannot be retried');
  assert.deepEqual(errors,[],'no production page errors');
  console.log('PASS: production HTML, canonical flags, authoritative country, bilingual search, selection/check/apply/error/STOP, cache, responsive geometry, navigation, repeated open and event-loop liveness.');
  console.log('Other mocked GET surfaces: '+JSON.stringify([...new Set(unhandled)]));
})().catch(async error=>{
  console.error(error);
  if(page){try{fs.mkdirSync(artifacts,{recursive:true});await page.screenshot({path:path.join(artifacts,'failure.png')});console.error('BODY '+(await page.locator('body').innerText()).slice(0,2000));console.error('PAGE ERRORS '+JSON.stringify(errors));console.error('VPN REQUESTS '+JSON.stringify(calls.filter(c=>c.path.startsWith('/api/vpn/'))));}catch(_){}}
  process.exitCode=1;
}).finally(async()=>{
  if(browser)await browser.close();
  fs.writeFileSync(addressFile+'.stop','stop');
  for(let i=0;i<40&&!serverExited;i++)await delay(100);
  if(!serverExited)server.kill('SIGTERM');
  if(serverLog.includes('FAIL'))console.error(serverLog);
});
