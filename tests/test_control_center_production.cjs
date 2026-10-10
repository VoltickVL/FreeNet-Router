// Complete browser flow against the REAL Go canonical HTML and asset handlers.
// Only runtime API JSON is mocked, using documentation-only addresses.
const {chromium}=require('playwright');
const assert=require('node:assert/strict');
const fs=require('node:fs'),path=require('node:path'),os=require('node:os');
const {spawn}=require('node:child_process');
const root=path.resolve(__dirname,'..'),artifacts=path.join(root,'test-artifacts','control-center');
const fixtureVersion=fs.readFileSync(path.join(root,'VERSION'),'utf8').trim().replace(/^v/i,'');
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'freenet-production-browser-'));
const addressFile=path.join(temp,'address');
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const server=spawn('go',['test','-run','^TestControlCenterBrowserServer$','-count=1','-timeout=5m'],{cwd:path.join(root,'freenet-ui'),env:{...process.env,FREENET_BROWSER_ADDRESS_FILE:addressFile},stdio:['ignore','pipe','pipe']});
let serverLog='',serverExited=false,browser,page;
server.stdout.on('data',b=>serverLog+=b);server.stderr.on('data',b=>serverLog+=b);server.on('exit',()=>serverExited=true);
const locations=[['be','Брюссель','Бельгия'],['de','Берлин','Германия'],['nl','Амстердам','Нидерланды'],['fi','Хельсинки','Финляндия'],['es','Мадрид','Испания']];
const profiles=Array.from({length:49},(_,i)=>{const[code,city,country]=locations[i%locations.length];return{id:`fixture-${i}`,name:`${code.toUpperCase()} ${city} ${i+1}, ${country}, Extra`,country_code:code,address:`192.0.2.${i+10}`,port:443}});
const ukraine={id:'fixture-ua',name:'UA Kyiv, Ukraine, Extra',country_code:'ua',address:'192.0.2.250',port:443};
const catalogProfiles=[...profiles,ukraine];
const current=profiles[0],target=profiles[1],ep=p=>`${p.address}:${p.port}`;
let status={version:fixtureVersion,country:'Бельгия',city:'Брюссель',country_code:'be',profile_label:current.name,endpoint:ep(current),xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',setup_complete:true,install_scenario:'existing_stack',subscription_configured:true,busy:false,updater_busy:false};
let planMode='ok',planDelay=0,applyMode='ok',currentCacheMode='strict',rttMode='ok';
const manualRTTToken='abcdef0123456789abcdef0123456789';
const calls=[],unhandled=[],errors=[];
const P='#fnVpnPickerV2Panel',T='#fnVpnPickerV2Toggle',S='#fnVpnPickerV2Search',R='#fnVpnPickerV2Results',F='#fnVpnPickerV2Footer',C='#fnVpnPickerV2Connect',RTT='#fnVpnPickerV2Refresh',RESIZE='#fnVpnPickerV2Resize';
const countApply=()=>calls.filter(c=>c.path==='/api/network-profile/apply').length;
// Playwright 1.51 waitForFunction's delayed predicate uses eval in the page.
// Poll from Node instead: production CSP remains untouched (no unsafe-eval).
async function until(predicate,label='condition',timeout=15000){
  const start=Date.now();
  while(Date.now()-start<timeout){if(await page.evaluate(predicate))return;await delay(100)}
  throw new Error('Timed out: '+label);
}
async function geometry(label){
  const data=await page.evaluate(({P,T,S,R,F,C})=>{
    const box=s=>{const n=document.querySelector(s),r=n.getBoundingClientRect();return{x:r.x,y:r.y,right:r.right,bottom:r.bottom,width:r.width,height:r.height,scrollWidth:n.scrollWidth,clientWidth:n.clientWidth,scrollHeight:n.scrollHeight,clientHeight:n.clientHeight}};
    return{panel:box(P),toggle:box(T),search:box(S),results:box(R),footer:box(F),connect:box(C),vw:innerWidth,vh:innerHeight,rootWidth:document.documentElement.scrollWidth,oldBackdrop:!!document.querySelector('#fnVpnPickerBackdrop')&&getComputedStyle(document.querySelector('#fnVpnPickerBackdrop')).display!=='none'};
  },{P,T,S,R,F,C});
  console.log('GEOMETRY '+label+' '+JSON.stringify(data));
  assert.ok(data.panel.x>=0&&data.panel.right<=data.vw+1&&data.panel.y>=0&&data.panel.bottom<=data.vh+1,label+' panel fits viewport');
  assert.ok(data.footer.bottom<=data.panel.bottom+1&&data.footer.y>=data.results.bottom-1,label+' footer below list and not clipped');
  assert.ok(data.connect.bottom<=data.panel.bottom+1,label+' connect visible');
  assert.ok(data.search.width>=data.panel.width*.83,label+' full-width search');
  assert.ok(data.results.scrollWidth<=data.results.clientWidth+1,label+' no horizontal list scroll');
  assert.ok(data.panel.scrollWidth<=data.panel.clientWidth+1,label+' no horizontal panel scroll');
  assert.equal(data.oldBackdrop,false,label+' no legacy fullscreen backdrop');return data;
}
async function capture(label){
  fs.mkdirSync(artifacts,{recursive:true});await page.screenshot({path:path.join(artifacts,label+'.png')});
  if(process.env.FREENET_PREVIEW_LOGS==='yes')console.log('FREENET_PREVIEW_'+label+'='+(await page.locator(P).screenshot({type:'jpeg',quality:38})).toString('base64'));
}
(async()=>{
  for(let i=0;i<600&&!fs.existsSync(addressFile);i++){if(serverExited)throw new Error(serverLog);await delay(100)}
  assert.ok(fs.existsSync(addressFile),'Go fixture startup: '+serverLog);
  const base=fs.readFileSync(addressFile,'utf8');browser=await chromium.launch({headless:true});
  page=await browser.newPage({viewport:{width:1440,height:1000}});page.setDefaultTimeout(12000);
  page.on('pageerror',e=>errors.push(e.message));
  const answer=(route,body,code=200)=>route.fulfill({status:code,contentType:'application/json',body:JSON.stringify(body)});
  await page.route('**/api/**',async route=>{
    const req=route.request(),url=new URL(req.url());
    if(url.pathname.includes('/assets/'))return route.continue();
    calls.push({path:url.pathname,method:req.method(),query:url.search,body:req.postData()});
    if(url.pathname==='/api/auth/status')return answer(route,{configured:true,authenticated:true});
    if(url.pathname==='/api/status')return answer(route,status);
    if(url.pathname==='/api/admin/dns')return answer(route,{success:true,host:url.searchParams.get('host'),resolver:'system',ips:['203.0.113.10'],mutation:'NONE'});
    if(url.pathname==='/api/admin/connector/grant')return answer(route,{success:true,active:false,transport:'LOCAL_LOOPBACK_ONLY',external_connected:false});
    if(url.pathname==='/api/admin/connector/readiness')return answer(route,{success:true,mutation:'NONE',router_os:'linux',router_arch:'mipsle',architecture_status:'OFFICIAL_BINARY_UNAVAILABLE',loopback_mcp:'http://127.0.0.1:1001/mcp',mcp_ready_on_router:true,grant_active:false,external_connected:false,external_evidence:'NOT_OBSERVED',next_step:'TRUSTED_EXTERNAL_HOST_AND_SECURE_BRIDGE_REQUIRED',notes:'MIPS: нужен доверенный узел; bridge ещё не реализован.'});
    if(url.pathname==='/api/admin/connector/host-preflight')return answer(route,{success:true,mutation:'NONE',router_os:'linux',router_arch:'mipsle',local_mcp:'CONFIG_ONLY',storage:'OBSERVED',free_bytes:4294967296,storage_exec:'EXEC_FLAG_ALLOWED',memory_available_bytes:251658240,memory_evidence:'PROC_MEMINFO',candidate:'ABSENT',preconditions:'STOP',executable_verified:false,openai_connected:false,chatgpt_connected:false,network_evidence:'NOT_TESTED',note:'STOP: архитектура не поддерживается.'});
    if(url.pathname==='/api/admin/connector/install/plan')return answer(route,{success:true,mutation:'NONE',ready:false,state:'STOP',architecture:'linux/mipsle',version:'v0.0.16',error:'Официального Linux tunnel-client для этой архитектуры нет',external_connected:false});
    if(url.pathname==='/api/admin/connector/tunnel/status')return answer(route,{success:true,configured:true,state:'RUNNING_NOT_READY',client_running:true,client_ready:false,external_mcp_verified:false});
    if(url.pathname==='/api/admin/connector/tunnel/health')return answer(route,{success:true,mutation:'NONE',health_state:'OBSERVED',client_ready:false,chatgpt_connected:false,
      control_plane:{status:'degraded',state:'backoff',reason_code:'http_error',failure_category:'http_error',http_status:403,consecutive_failures:3},
      mcp:{status:'unknown',state:'starting',startup_probe:'pending'},
      oauth:{status:'unknown'},cloudflared:{status:'disabled'},response_delivery:{status:'unknown'}});
    if(url.pathname==='/api/admin/connector/access')return answer(route,{success:true,state:'PENDING',request_id:'c'.repeat(64),expires_at:'2026-10-10T23:59:00Z'});
    if(url.pathname==='/api/admin/connector/access/approve')return answer(route,{success:true,state:'APPROVED',mutation:'AUTHORIZATION_ONLY'});
    if(url.pathname==='/api/admin/connector/access/revoke')return answer(route,{success:true,state:'DENIED',mutation:'AUTHORIZATION_ONLY'});

    if(url.pathname==='/api/admin/connector/pair')return answer(route,{success:true,token:'a'.repeat(64),scope:'read:diagnostics',transport:'LOCAL_LOOPBACK_ONLY',mutation:'AUTHORIZATION_ONLY'});
    if(url.pathname==='/api/admin/connector/revoke')return answer(route,{success:true,active:false,mutation:'AUTHORIZATION_ONLY'});
    if(url.pathname==='/api/admin/route')return answer(route,{success:true,host:url.searchParams.get('host'),client:url.searchParams.get('client')||'',port:443,network:'tcp',expected_action:'UNKNOWN',confidence:'UNKNOWN',observed_route:'NOT_OBSERVED',rules_sha256:'fixture-routing-sha',reason:'GeoSite требует точного сопоставления',mutation:'NONE'});
    if(url.pathname==='/api/journal/diagnostics')return answer(route,{schema:1,version:'v0.7.4',generated_at:'2026-10-10T00:00:00Z',technical_events:[]});
    if(url.pathname==='/api/operation/state')return answer(route,{success:true,active:false});
    if(url.pathname==='/api/subscription')return answer(route,{success:true,configured:true});
    if(url.pathname==='/api/vpn/current-quality'&&url.searchParams.get('job')==='cache'){
      const p=profiles.find(p=>ep(p)===status.endpoint)||current;
      const candidate=currentCacheMode==='fallback'
        ?{...p,endpoint:status.endpoint,current:true,tested:true,eligible:false,available:true,reachable:true,download_mbps:0,fallback_download_mbps:37.4,throughput_source:'current_fallback',application_rtt_ms:105,tcp_rtt_ms:70,jitter_ms:5,media_samples:0,media_stalls:0,service_ok:4,service_total:4}
        :{...p,endpoint:status.endpoint,current:true,tested:true,eligible:true,available:true,reachable:true,download_mbps:55,throughput_source:'strict_aggregate',application_rtt_ms:105,tcp_rtt_ms:70,jitter_ms:5,media_samples:4,media_stalls:0,service_ok:4,service_total:4};
      return answer(route,{success:true,available:candidate.eligible,scanned_at:new Date().toISOString(),candidates:[candidate]});
    }
    if(url.pathname==='/api/provider-profiles/rtt'){
      const measuredProfiles=profiles.map((p,i)=>rttMode==='rotate'&&i===1?{...p,address:'192.0.2.254'}:{...p});
      const catalog=measuredProfiles.map(p=>({id:p.id,name:p.name,country_code:p.country_code,address:p.address,port:p.port}));
      const results=measuredProfiles.map((p,i)=>i===7
        ?{profile_id:p.id,endpoint:ep(p),reachable:false,attempted:false,status:'unknown'}
        :i===8
          ?{profile_id:p.id,endpoint:ep(p),reachable:false,attempted:true,status:'unreachable'}
          :{profile_id:p.id,endpoint:rttMode==='malformed'&&i===1?'192.0.2.253:443':ep(p),reachable:true,attempted:true,status:'reachable',rtt_ms:55+((48-i)*4),jitter_ms:i%9});
      return answer(route,{success:true,catalog,catalog_key:'fixture-catalog',selection_token:manualRTTToken,results,profiles:profiles.length,unique_endpoints:profiles.length,checked:48,reachable:47,unknown:1,partial:true,probe_mode:'proxy_http_multi_origin',fresh:true,mutation:'NONE'});
    }
    if(url.pathname==='/api/provider-profile/plan'){
      const id=url.searchParams.get('profile_id');
      if(planMode==='offline')return answer(route,{success:false,profile_id:id,candidate_xray_valid:false,mutation:'NONE',error:'Fixture subscription unavailable'},503);
      if(planDelay)await delay(planDelay);
      const selected=profiles.find(p=>p.id===id);
      const provider_plan=planMode==='error'
        ?{success:false,profile_id:id,candidate_xray_valid:false,mutation:'NONE',error:'Конфигурация сервера не прошла проверку Xray. Текущий VPN не изменён.'}
        :{success:true,profile_id:id,candidate_xray_valid:true,mutation:'NONE',endpoint:ep(selected||target)};
      return answer(route,provider_plan,provider_plan.success?200:409);
    }
    if(url.pathname==='/api/network-profile/plan'){
      const id=url.searchParams.get('provider_profile_id');
      if(planMode==='offline')return answer(route,{success:false,error:'Fixture subscription unavailable'},503);
      if(id&&planDelay)await delay(planDelay);
      const selected=profiles.find(p=>p.id===id);
      const provider_plan=!id?undefined:planMode==='error'?{success:false,candidate_xray_valid:false,mutation:'NONE',error:'Конфигурация сервера не прошла проверку Xray. Текущий VPN не изменён.'}:{success:true,candidate_xray_valid:true,mutation:'NONE',endpoint:ep(selected||target)};
      return answer(route,{success:true,supported:true,active:true,provider_plan,extra_profiles:catalogProfiles});
    }
    if(url.pathname==='/api/network-profile/apply'){
      const body=JSON.parse(req.postData());
      const expectedKeys=body.selection_token?['confirm','operation','profile_id','selection_token']:['confirm','operation','profile_id'];
      assert.deepEqual(Object.keys(body).sort(),expectedKeys);
      if(body.selection_token)assert.equal(body.selection_token,manualRTTToken,'manual apply must bind the RTT snapshot');
      assert.equal(body.operation,'provider');assert.equal(body.confirm,true);
      const selected=profiles.find(p=>p.id===body.profile_id);assert.ok(selected);
      if(applyMode==='unknown')return answer(route,{success:false,error:'Результат операции не подтверждён',rollback_state:'UNKNOWN'},502);
      await delay(250);status={...status,country:'',city:'',country_code:'',profile_label:selected.name,endpoint:ep(selected)};
      return answer(route,{success:true,applied:true,rollback_state:'NOT_NEEDED'});
    }
    if(url.pathname==='/api/xray/service')return answer(route,{success:true,version:'v26.9.9',current_version:'v26.9.9',online:true});
    if(url.pathname==='/api/settings-v3')return answer(route,{success:true,auto_vpn:{enabled:true,country_scope:'region',countries:[]},automation:{success:true,current_profile:status.profile_label,current_endpoint:status.endpoint,country_code:status.country_code,settings:{enabled:true},events:[]},subscription:{enabled:false,interval:'6h'},geodata:{enabled:true,interval:'3h'},freenet:{enabled:false,interval:'12h'},backup:{enabled:false,interval:'24h'},events:[]});
    if(url.pathname==='/api/automation')return answer(route,{success:true,settings:{enabled:true,country_scope:'region',countries:[]},current_profile:status.profile_label,current_endpoint:status.endpoint,country_code:status.country_code,events:[]});
    if(url.pathname==='/api/settings-v3/countries')return answer(route,{success:true,countries:[...locations.map(([code,,name])=>({code,name,available:true})),{code:'ua',name:'Украина',available:true}],selected:['ua'],fresh:true});
    if(url.pathname==='/api/geodata/files')return answer(route,{success:true,files:[],search_enabled:false});
    if(url.pathname.startsWith('/api/system/update/'))return answer(route,{success:true,ready:true,state:'IDLE',current_version:'v'+fixtureVersion,latest_version:'v'+fixtureVersion,update_available:false,rollback_state:'NOT_NEEDED'});
    if(req.method()!=='GET')throw new Error('Unexpected mutation '+url.pathname);
    unhandled.push(url.pathname);return answer(route,{success:true,available:false,configured:true,active:false,events:[]});
  });
  const verifyCanonicalSidebar = async context => {
    const actual = await page.locator('.sidebar .nav>.nav-btn[data-page]').evaluateAll(nodes => nodes.map(n => n.dataset.page === 'routing' ? 'network' : n.dataset.page));
    assert.deepEqual([...actual].sort(), [...['overview','settings','network','journal','admin']].sort(), context+': exactly five canonical sidebar entries, no duplicates or retired routes');
    assert.equal(await page.locator('.sidebar .nav-btn[data-page="subscription"]').count(),0,context+': legacy Subscription must not reappear');
  };
  await page.goto(base);await until(()=>document.documentElement.dataset.freenetCanonicalReady==='1','canonical boot');
  await verifyCanonicalSidebar('cold start');
  await page.locator('.nav-btn[data-page="admin"]').click();
  await until(()=>document.querySelector('[data-page-view="admin"]')?.classList.contains('active'),'Administration visible');
  await page.locator('#adminCommandInput').fill('dns sg.api.io.mi.com');
  await page.locator('#adminRunBtn').click();
  await until(()=>document.querySelector('#adminCommandOutput')?.textContent.includes('203.0.113.10'),'DNS diagnostic output');
  assert.match(await page.locator('#adminCommandOutput').textContent(),/не определён/, 'DNS must not claim Xray routing');
  await page.locator('#adminCommandInput').fill('ssh cat /etc/passwd');
  await page.locator('#adminRunBtn').click();
  assert.match(await page.locator('#adminCommandOutput').textContent(),/не поддерживается/, 'raw SSH must be rejected');
  assert.equal(calls.filter(c=>c.path==='/api/admin/dns').length,1,'only allowlisted DNS diagnostic request');
  await page.locator('#connectorReadinessBtn').click();
  await until(()=>document.querySelector('#connectorReadinessOutput')?.textContent.includes('официального tunnel-client'),'MIPS tunnel readiness STOP');
  assert.match(await page.locator('#connectorReadinessOutput').textContent(),/НЕ ПОДТВЕРЖДЕНО/,'must not invent connected tunnel');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/readiness').length,1,'browser preflight should be GET only');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/readiness')?.method,'GET','preflight no mutation');
  await page.locator('#connectorTunnelSection summary').click();
  await page.locator('#connectorTunnelStatusBtn').click();
  await until(()=>document.querySelector('#connectorTunnelOutput')?.textContent.includes('HTTP 403'),'component health readiness diagnosis in browser');
  const tunnelOut=await page.locator('#connectorTunnelOutput').textContent();
  assert.match(tunnelOut,/RUNNING_NOT_READY/,'runtime remains not ready');
  assert.match(tunnelOut,/OpenAI control-plane: degraded/,'component reason visible');
  assert.match(tunnelOut,/MCP: unknown/,'MCP startup visible');
  assert.doesNotMatch(tunnelOut,/tunnel_[a-z0-9]+|sk-[a-z0-9]+/i,'no secrets in health');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/tunnel/health')?.method,'GET','component health must remain read-only');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/tunnel/start').length,0,'health check cannot restart OpenAI client');
  assert.match(await page.locator('#connectorTunnelOutput').textContent(),/НЕ ПОДТВЕРЖДЁН/,'never declare ChatGPT connection');
  await page.locator('#connectorAccessSection summary').click();
  await page.locator('#connectorAccessStatusBtn').click();
  await until(()=>document.querySelector('#connectorAccessOutput')?.textContent.includes('PENDING'),'incoming request visible to user');
  assert.equal(await page.locator('#connectorAccessApproveBtn').isDisabled(),false,'explicit approval available on pending request');
  page.once('dialog',d=>d.accept());
  await page.locator('#connectorAccessApproveBtn').click();
  await until(()=>document.querySelector('#connectorAccessOutput')?.textContent.includes('APPROVED'),'access approved by owner');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/access/approve')?.method,'POST','approval must require POST');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/tunnel/start').length,0,'approval cannot start tunnel');
  page.once('dialog',d=>d.accept());
  await page.locator('#connectorAccessRevokeBtn').click();
  await until(()=>document.querySelector('#connectorAccessOutput')?.textContent.includes('отозвано'),'owner immediate revoke');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/access/revoke')?.method,'POST','revoke requires POST');

  await page.locator('#connectorHostPreflightBtn').click();
  await until(()=>document.querySelector('#connectorHostPreflightOutput')?.textContent.includes('Предпосылки: STOP'),'host preflight resource summary');
  const hostOut=await page.locator('#connectorHostPreflightOutput').textContent();
  assert.match(hostOut,/Предпосылки: STOP/,'unsupported MIPS remains STOP despite available flash');
  assert.match(hostOut,/ChatGPT: НЕ ПОДТВЕРЖДЕНО/,'host preflight must not claim active tunnel');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/host-preflight').length,1,'one read-only host request');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/host-preflight')?.method,'GET','host preflight must not mutate');
  await page.locator('#connectorInstallPlanBtn').click();
  await until(()=>document.querySelector('#connectorInstallOutput')?.textContent.includes('STOP:'),'unsupported binary install STOP');
  assert.equal(await page.locator('#connectorInstallBtn').isDisabled(),true,'MIPS install must be disabled');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/install/plan').length,1,'one read-only install plan');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/install/apply').length,0,'no installation on unsupported architecture');
  await page.locator('#connectorStatusBtn').click();
  await until(()=>document.querySelector('#connectorGrantState')?.textContent.includes('Локальный доступ выключен'),'connector status read-only');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/grant').length,1,'connector status uses only read-only GET');
  // The production-browser fixture runs on localhost, where local-only pairing
  // is allowed. A remote HTTP LAN host is intentionally rejected by the UI.
  page.once('dialog',d=>d.accept());
  await page.locator('#connectorPairBtn').click();
  await until(()=>document.querySelector('#connectorGrantSecret')?.textContent.includes('a'.repeat(64)),'one-time local grant');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/pair').length,1,'localhost grant requires explicit one-time POST');
  await page.locator('.nav-btn[data-page="overview"]').click();
  assert.equal(await page.locator('#connectorGrantSecret').textContent(),'','one-time token must be erased on navigation');
  await page.locator('.nav-btn[data-page="admin"]').click();
  page.once('dialog',d=>d.accept());
  await page.locator('#connectorRevokeBtn').click();
  await until(()=>document.querySelector('#connectorGrantOutput')?.textContent.includes('Локальный доступ отозван'),'connector explicit revoke');
  assert.equal(calls.filter(c=>c.path==='/api/admin/connector/revoke').length,1,'revocation requires one explicit POST');
  assert.equal(calls.find(c=>c.path==='/api/admin/connector/revoke')?.body,'{"confirm":true}','revoke requires confirmation payload');
  assert.equal(await page.locator('#connectorGrantSecret').isHidden(),true,'one-time token is not shown without secure pairing');
  await page.locator('#adminRouteHost').fill('sg.api.io.mi.com');
  await page.locator('#adminRouteClient').fill('192.168.50.144');
  await page.locator('#adminRouteBtn').click();
  await until(()=>document.querySelector('#adminRouteOutput')?.textContent.includes('GeoSite требует точного сопоставления'),'read-only route preview');
  assert.match(await page.locator('#adminRouteOutput').textContent(),/НЕИЗВЕСТНО/,'ambiguous rule must not claim DIRECT');
  assert.match(await page.locator('#adminRouteOutput').textContent(),/НЕ НАБЛЮДАЛСЯ/,'route preview must distinguish observed LAN traffic');
  assert.equal(calls.filter(c=>c.path==='/api/admin/route').length,1,'exactly one read-only route request');
  assert.equal(calls.find(c=>c.path==='/api/admin/route')?.method,'GET','route diagnostics must not mutate runtime');
  await page.locator('.nav-btn[data-page="overview"]').click();
  await page.locator(T).waitFor({state:'visible'});await until(()=>document.querySelector('#fnVpnPickerV2Country')?.textContent==='Бельгия','current country');
  assert.equal(await page.locator('#fnVpnPickerToggle').count(),0,'legacy picker owner retired');assert.equal(await page.locator(T).count(),1);assert.equal(await page.locator(P).isHidden(),true);
  assert.equal(countApply(),0,'no startup apply');assert.equal(calls.filter(c=>c.path.startsWith('/api/vpn/')&&!c.query.includes('job=cache')).length,0,'cache read does not start speed scan');assert.deepEqual(errors,[]);
  status={...status,country:'',country_code:'',city:''};await page.evaluate(()=>loadStatus());
  await until(()=>document.querySelector('#fnVpnPickerV2Flag')?.dataset.country==='be','profile identity fallback');
  await until(()=>document.querySelector('#bestCurrentFlag')?.classList.contains('flag-be'),'Overview current flag');
  assert.equal(await page.locator('#fnVpnPickerV2Country').textContent(),'Бельгия');
  await until(()=>/55\.0 Мбит\/с/.test(document.querySelector('#bestCurrentMetrics')?.textContent||''),'strict current speed value');
  assert.match(await page.locator('#bestCurrentMetrics').textContent(),/55\.0 Мбит\/с/);
  currentCacheMode='fallback';
  await page.reload();await until(()=>document.documentElement.dataset.freenetCanonicalReady==='1','legacy fallback canonical boot');
  await verifyCanonicalSidebar('first reload');
  await until(()=>/Скорость VPN/.test(document.querySelector('#bestCurrentMetrics')?.textContent||''),'strict-only current speed UI');
  const legacyFallbackMetrics=await page.locator('#bestCurrentMetrics').textContent();
  assert.doesNotMatch(legacyFallbackMetrics,/37\.4 Мбит\/с/,'legacy fallback throughput must never be displayed');
  assert.doesNotMatch(legacyFallbackMetrics,/Быстрый|не для сравнения/i,'legacy fallback UX must be retired');
  assert.match(legacyFallbackMetrics,/Скорость VPN[\s\S]*—/,'missing canonical speed must remain unknown');
  currentCacheMode='strict';
  await page.reload();await until(()=>document.documentElement.dataset.freenetCanonicalReady==='1','strict canonical reboot');
  await verifyCanonicalSidebar('second reload');
  await page.locator(T).waitFor({state:'visible'});
  await until(()=>document.querySelector('#fnVpnPickerV2Country')?.textContent==='Бельгия','current country after provenance reload');
  const order=await page.locator('#overviewApprovedTop').evaluate(n=>Array.from(n.children).map(x=>x.matches('.fn-xray-topbar')?'xray':x.id==='fnVpnPickerV2Host'?'vpn':x.id==='topFreenetUpdate'?'freenet':/DNS/i.test(x.textContent||'')?'dns':'other').filter(x=>x!=='other'));
  assert.deepEqual(order,['xray','vpn','dns','freenet']);
  const topbarType=await page.evaluate(()=>{
    const selectors=['#xrayTopbarVersion','#overviewApprovedTop .fn-shell-fact-copy>strong','#topFreenetUpdate .fn-version-copy strong'];
    return selectors.map(selector=>{
      const node=document.querySelector(selector);
      return node ? getComputedStyle(node).fontSize : null;
    });
  });
  assert(topbarType.every(Boolean),'all topbar version/value labels must exist: '+JSON.stringify(topbarType));
  assert.equal(new Set(topbarType).size,1,'Xray, DNS, FreeNet topbar labels must share one font size: '+JSON.stringify(topbarType));
  const valueTypography = await page.evaluate(() => {
    const selectors=['#xrayTopbarVersion','#fnVpnPickerV2Country','#overviewApprovedTop .fn-shell-dns .fn-shell-fact-copy>strong','#topFreenetUpdate .fn-version-copy strong'];
    return selectors.map(selector=>{
      const el=document.querySelector(selector);
      if(!el) return null;
      const style=getComputedStyle(el);
      return {size:style.fontSize,weight:style.fontWeight};
    });
  });
  assert(valueTypography.every(Boolean),'four canonical topbar value labels must exist: '+JSON.stringify(valueTypography));
  assert.equal(new Set(valueTypography.map(x=>x.size)).size,1,'topbar values must be same size as VPN country: '+JSON.stringify(valueTypography));
  assert.equal(new Set(valueTypography.map(x=>x.weight)).size,1,'topbar weights must match VPN: '+JSON.stringify(valueTypography));
  assert.equal(valueTypography[1].size,'12px','VPN and other values must use compact 12px rather than oversized 14.5px');

  const rttCallsBeforeOpen=calls.filter(c=>c.path==='/api/provider-profiles/rtt').length;
  await page.locator(T).click();await page.locator(P).waitFor({state:'visible'});await until(()=>document.querySelectorAll('#fnVpnPickerV2Results button').length===49,'49 profiles');
  assert.equal(await page.locator(R+' [data-profile-id="fixture-ua"]').count(),0,'Ukraine must be excluded from manual replacement picker');
  assert.equal(await page.locator(RTT).isVisible(),true,'RTT refresh must be visible next to close');
  await delay(250);
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen,'opening picker must remain network-idle and must not auto-run RTT');
  const initialRTT=await page.locator(R+' .fnv2-rtt').evaluateAll(nodes=>nodes.map(n=>n.textContent||''));
  assert.ok(initialRTT.every(v=>v===''),'selector must show no invented RTT before explicit refresh');

  const planCallsBeforeSynthetic=calls.filter(c=>c.path==='/api/network-profile/plan').length;
  await page.evaluate(()=>document.querySelector('#fnVpnPickerV2Refresh').click());
  await delay(120);
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen,'synthetic refresh must not issue RTT request');
  assert.equal(calls.filter(c=>c.path==='/api/network-profile/plan').length,planCallsBeforeSynthetic,'opening/synthetic refresh must not hydrate network plan');

  await page.locator(RTT).click();
  for(let i=0;i<100&&calls.filter(c=>c.path==='/api/provider-profiles/rtt').length<rttCallsBeforeOpen+1;i++)await delay(25);
  await until(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled,'forced RTT refresh complete');
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen+1,'one manual RTT refresh must issue exactly one read-only sweep');
  assert.ok(calls.filter(c=>c.path==='/api/provider-profiles/rtt').at(-1).query.includes('refresh=1'),'manual RTT refresh must bypass cached evidence');
  await until(()=>document.querySelectorAll('#fnVpnPickerV2Results .fnv2-rtt').length===49 && /VPN-пинг:/.test(document.querySelector('#fnVpnPickerV2RTTState')?.textContent||''),'manual RTT scan');
  let rttOrder=await page.locator(R+' button').evaluateAll(nodes=>nodes.slice(0,4).map(n=>({id:n.dataset.profileId,rtt:n.querySelector('.fnv2-rtt')?.textContent})));
  assert.deepEqual(rttOrder.map(x=>x.id),['fixture-48','fixture-47','fixture-46','fixture-45'],'manual RTT refresh must sort ascending RTT: '+JSON.stringify(rttOrder));
  assert.ok(rttOrder.every(x=>/мс/.test(x.rtt||'')),'RTT must be visible beside every measured sorted row: '+JSON.stringify(rttOrder));
  assert.match(await page.locator('#fnVpnPickerV2RTTState').textContent(),/завершён частично/,'partial RTT must be explicit');
  assert.equal(await page.locator(R+' [data-profile-id="fixture-7"] .fnv2-rtt').textContent(),'не проверен','deadline-unknown profile must not be labeled dead');
  assert.equal(await page.locator(R+' [data-profile-id="fixture-8"] .fnv2-rtt').textContent(),'нет ответа','attempted negative profile remains explicit');

  await page.locator('#fnVpnPickerV2Close').click();
  const planCallsBeforeReopen=calls.filter(c=>c.path==='/api/network-profile/plan').length;
  await page.locator(T).click();await page.locator(P).waitFor({state:'visible'});
  await delay(250);
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen+1,'reopening picker must not start another RTT sweep');
  assert.equal(calls.filter(c=>c.path==='/api/network-profile/plan').length,planCallsBeforeReopen,'reopening picker must remain network-idle');

  rttMode='rotate';
  await page.locator(RTT).click();
  await until(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled,'rotated RTT refresh complete');
  assert.doesNotMatch(await page.locator('#fnVpnPickerV2RTTState').textContent(),/Список VPN изменился/,'endpoint rotation before RTT sweep is a valid new snapshot');
  assert.equal(await page.locator(R+' [data-profile-id="fixture-1"] small').textContent(),'192.0.2.254:443','selector must adopt exact endpoint snapshot measured by RTT API');
  assert.match(await page.locator(R+' [data-profile-id="fixture-1"] .fnv2-rtt').textContent(),/мс/,'rotated snapshot RTT must remain attached');

  rttMode='malformed';
  await page.locator(RTT).click();
  await until(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled,'malformed RTT refresh complete');
  assert.match(await page.locator('#fnVpnPickerV2RTTState').textContent(),/Список VPN изменился/,'internally inconsistent RTT/catalog response must fail closed');
  const rejectedRTT=await page.locator(R+' .fnv2-rtt').evaluateAll(nodes=>nodes.map(n=>n.textContent||''));
  assert.ok(rejectedRTT.every(v=>v===''),'inconsistent snapshot must not attach RTT from another snapshot');
  assert.doesNotMatch(await page.locator('#fnVpnPickerV2RTTState').textContent(),/проверено .* ответили/i,'inconsistent snapshot must not publish aggregate summary');

  rttMode='ok';
  await page.locator(RTT).click();
  await until(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled,'baseline RTT refresh complete');
  assert.equal(await page.locator(R+' [data-profile-id="fixture-1"] small').textContent(),ep(profiles[1]),'baseline refresh must restore current safe catalog snapshot');
  const initial=await geometry('desktop-initial');assert.ok(initial.panel.y>=initial.toggle.bottom,'anchored below VPN');assert.ok(initial.results.height>=400,'desktop list must use available viewport height: '+JSON.stringify(initial));
  assert.equal(await page.locator(RESIZE).isVisible(),true,'desktop picker must expose one vertical resize affordance');
  const resizeBox=await page.locator(RESIZE).boundingBox();assert.ok(resizeBox,'desktop resize handle missing');
  await page.mouse.move(resizeBox.x+resizeBox.width/2,resizeBox.y+resizeBox.height/2);
  await page.mouse.down();await page.mouse.move(resizeBox.x+resizeBox.width/2,resizeBox.y+resizeBox.height/2+110,{steps:5});await page.mouse.up();
  const resized=await geometry('desktop-resized');
  assert.ok(resized.panel.height>initial.panel.height+50,'desktop picker resize must increase useful list height: '+JSON.stringify({initial:initial.panel,resized:resized.panel}));
  assert.ok(resized.panel.bottom<=resized.vh+1,'resized picker must remain viewport-bounded');
  const flags=await page.evaluate(()=>{
    const a=document.querySelector('#fnVpnPickerV2Flag'),b=document.querySelector('#fnVpnPickerV2CurrentFlag'),c=document.querySelector('#bestCurrentFlag');
    return{source:a.dataset.flagSource,chip:getComputedStyle(a).backgroundImage,panel:getComputedStyle(b).backgroundImage,overview:getComputedStyle(c).backgroundImage,all:Array.from(document.querySelectorAll('#fnVpnPickerV2Results .fnv2-flag')).every(n=>n.dataset.flagSource==='canonical'),emoji:/[\u{1F1E6}-\u{1F1FF}]/u.test(document.querySelector('#fnVpnPickerV2Panel').textContent)};
  });
  assert.equal(flags.source,'canonical');assert.equal(flags.chip,flags.overview);assert.equal(flags.panel,flags.overview);assert.equal(flags.all,true);assert.equal(flags.emoji,false);await capture('desktop-list');
  for(const query of ['герм','Germany','DE']){await page.locator(S).fill(query);assert.ok(await page.locator(R+' button').count()>0,'search '+query);assert.equal(await page.locator(R+' .fnv2-flag:not([data-country="de"])').count(),0)}
  await page.locator(S).fill('Мадрид');assert.ok(await page.locator(R+' button').count()>0,'Madrid must be searchable');assert.equal(await page.locator(R+' .fnv2-flag:not([data-country="es"])').count(),0,'Madrid must use canonical Spain flag');assert.equal(await page.locator(R+' .fnv2-flag[data-flag-source="canonical"]').count(),await page.locator(R+' button').count(),'Spain flag must not fall back to legacy/unknown renderer');
  await page.locator(S).fill('nonexistent-fixture');assert.equal(await page.locator(R+' button').count(),0);assert.equal(await page.locator(R+' .fnv2-empty').isVisible(),true);
  await page.locator(S).fill('DE');planDelay=1300;
  const providerReadsBeforeManual=calls.filter(c=>c.path==='/api/provider-profile/plan').length;
  await page.locator(R+' button[data-profile-id="fixture-1"]').click();
  await until(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='ready','RTT-ready');
  assert.equal(calls.filter(c=>c.path==='/api/provider-profile/plan').length,providerReadsBeforeManual,
    'successful RTT selection must not request a second provider plan, even with a slow subscription');
  assert.equal(await page.locator(C).isDisabled(),false,'manual RTT click must enable Connect immediately');
  assert.equal(await page.locator(C).textContent(),'Подключиться');
  assert.equal(await page.locator('#fnVpnPickerV2Country').textContent(),'Бельгия');
  await geometry('ready');await capture('desktop-ready');assert.equal(countApply(),0);
  await page.locator(C).click();await page.locator(C).dispatchEvent('click');
  await until(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Германия','connected country');await until(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='success','verified apply');
  assert.equal(countApply(),1,'double click cannot apply twice');assert.equal(await page.locator(C).isDisabled(),true);
  await page.locator('#fnVpnPickerV2Reset').click();planDelay=0;planMode='error';
  // Explicitly remove the measured snapshot to test the legacy no-RTT fallback.
  await page.evaluate(()=>{window.freenetManualRTTSelection=null;});
  await page.locator(S).fill('NL');await page.locator(R+' button').first().click();
  await until(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='error','validation error');assert.equal(await page.locator(C).isDisabled(),true);assert.match(await page.locator('#fnVpnPickerV2Detail').textContent(),/Xray/);await geometry('validation-error');assert.equal(countApply(),1);
  await page.locator('#fnVpnPickerV2Reset').click();planMode='offline';await page.evaluate(()=>loadNetworkPlan());await page.locator(S).fill('');
  await until(()=>document.querySelectorAll('#fnVpnPickerV2Results button').length===49,'cached list');await until(()=>!document.querySelector('#fnVpnPickerV2Stale').hidden,'stale banner');assert.equal(countApply(),1);

  // Real-upgrade regression: an exact Swiss profile may share an endpoint with
  // stale Belgian rows. Current logical identity must come from the exact
  // profile label, never endpoint equality or browser cache.
  const connectedStatus={...status};
  status={...status,country:'',country_code:'',city:'',profile_label:'🇨🇭 Цюрих, Швейцария, Extra',endpoint:ep(current)};
  await page.evaluate(()=>loadStatus());
  await until(()=>document.querySelector('#fnVpnPickerV2Flag')?.dataset.country==='ch','Swiss exact current identity');
  assert.equal(await page.locator('#fnVpnPickerV2Country').textContent(),'Швейцария');
  assert.match(await page.locator('.fnv2-current-copy').textContent(),/Цюрих, Швейцария/);
  assert.equal(countApply(),1,'identity reconcile must not mutate VPN');

  // A stale cache is presentation-only. Reopening the picker must stay
  // network-idle; only an explicit trusted RTT refresh may replace it with the
  // exact fresh catalog snapshot that was actually measured.
  planMode='ok';
  await page.keyboard.press('Escape');
  const planReadsBeforeReopen=calls.filter(c=>c.path==='/api/network-profile/plan'&&c.method==='GET'&&!c.query.includes('provider_profile_id')).length;
  const rttReadsBeforeStaleReopen=calls.filter(c=>c.path==='/api/provider-profiles/rtt').length;
  await page.locator(T).click();await page.locator(P).waitFor({state:'visible'});
  await delay(250);
  assert.equal(await page.locator('#fnVpnPickerV2Stale').isVisible(),true,'stale cache must remain explicitly stale until trusted refresh');
  const planReadsAfterReopen=calls.filter(c=>c.path==='/api/network-profile/plan'&&c.method==='GET'&&!c.query.includes('provider_profile_id')).length;
  assert.equal(planReadsAfterReopen,planReadsBeforeReopen,'stale reopen must not perform hidden network-plan refresh');
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttReadsBeforeStaleReopen,'stale reopen must not auto-run RTT');

  rttMode='ok';
  await page.locator(RTT).click();
  await until(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled,'explicit stale catalog refresh complete');
  await until(()=>document.querySelector('#fnVpnPickerV2Stale')?.hidden===true,'fresh measured catalog replaces stale cache');
  assert.equal(calls.filter(c=>c.path==='/api/provider-profiles/rtt').length,rttReadsBeforeStaleReopen+1,'explicit refresh must issue one RTT/catalog request');
  assert.equal(calls.filter(c=>c.path==='/api/network-profile/plan'&&c.method==='GET'&&!c.query.includes('provider_profile_id')).length,planReadsBeforeReopen,'explicit RTT catalog refresh must not require hidden network-plan hydrate');
  assert.equal(countApply(),1,'catalog refresh must remain read-only');
  await page.keyboard.press('Escape');
  for(const viewport of [{width:1920,height:1000},{width:1600,height:900},{width:1440,height:900},{width:1228,height:900},{width:1180,height:900},{width:1024,height:800},{width:980,height:800},{width:844,height:390},{width:820,height:800},{width:760,height:700},{width:390,height:844}]){
    await page.setViewportSize(viewport);
    const shell=await page.evaluate(()=>{const top=document.querySelector('.topbar'),summary=document.querySelector('#overviewApprovedTop'),head=document.querySelector('.page[data-page-view="overview"] .page-head');const tr=top.getBoundingClientRect(),hr=head.getBoundingClientRect(),shown=n=>!!n&&getComputedStyle(n).display!=='none';return{summaryDisplay:getComputedStyle(summary).display,vpnVisible:shown(document.querySelector('#fnVpnPickerV2Host')),xrayVisible:shown(summary.querySelector('.fn-xray-topbar')),factsVisible:Array.from(summary.querySelectorAll('.overview-approved-fact')).some(shown),freenetVisible:shown(summary.querySelector('#topFreenetUpdate')),topBottom:tr.bottom,headTop:hr.top,topScrollWidth:top.scrollWidth,topClientWidth:top.clientWidth,summaryRight:summary.getBoundingClientRect().right,contentRight:document.querySelector('#controlCenter .main .content').getBoundingClientRect().right};});
    if(viewport.width>760)assert.ok(Math.abs(shell.summaryRight-shell.contentRight)<=2, `${viewport.width}px статусные карточки топбара должны заканчиваться по правому краю центральной колонки: ${JSON.stringify(shell)}`);
    if(viewport.width<=760){assert.notEqual(shell.summaryDisplay,'none',`${viewport.width}px mobile topbar must retain the VPN picker`);assert.equal(shell.vpnVisible,true,`${viewport.width}px mobile VPN picker must remain accessible`);assert.equal(shell.xrayVisible,true,`${viewport.width}px mobile Xray status must remain visible`);assert.equal(shell.factsVisible,true,`${viewport.width}px mobile DNS status must remain visible`);assert.equal(shell.freenetVisible,true,`${viewport.width}px mobile FreeNet version must remain visible`);assert.ok(shell.topScrollWidth<=shell.topClientWidth+1,`${viewport.width}px mobile topbar must not overflow horizontally`);assert.ok(shell.topBottom-shell.headTop<=1,`${viewport.width}px topbar must not cover Overview headline`);assert.ok(shell.headTop>=shell.topBottom-1,`${viewport.width}px Overview content must start below topbar`);}else{assert.notEqual(shell.summaryDisplay,'none',`${viewport.width}px non-mobile topbar must retain status summary`);}
    await page.locator(T).click();await page.locator(P).waitFor({state:'visible'});await delay(100);await geometry(`${viewport.width}x${viewport.height}`);if(viewport.width<=760)assert.equal(await page.locator(RESIZE).isHidden(),true,`${viewport.width}px mobile picker must stay auto-sized and non-resizable`);if(viewport.width===390)await capture('mobile-list');await page.keyboard.press('Escape');assert.equal(await page.locator(P).isHidden(),true);
  }
  await page.setViewportSize({width:1440,height:1000});await page.evaluate(()=>setPage('settings'));await delay(300);await page.locator(T).click();await geometry('settings-route');await page.keyboard.press('Escape');await page.evaluate(()=>setPage('overview'));
  for(let i=0;i<10;i++){await page.locator(T).click();await page.keyboard.press('Escape')}
  await page.evaluate(()=>new Promise(resolve=>{const n=document.createElement('div');document.body.appendChild(n);for(let i=0;i<100;i++)n.textContent=String(i);setTimeout(()=>{n.remove();resolve()},80)}));
  assert.equal(await page.locator(T).count(),1);assert.equal(await page.locator(P).count(),1);assert.equal(countApply(),1);
  status={...connectedStatus,xray_online:false};await page.evaluate(()=>loadStatus());await until(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Не подключён','offline');await page.locator(T).click();assert.equal(await page.locator('#fnVpnPickerV2State').getAttribute('data-online'),'false');
  status={...status,xray_online:true};await page.evaluate(()=>loadStatus());await until(()=>document.querySelector('#fnVpnPickerV2Country').textContent==='Германия','online again');await page.locator(S).fill('FI');await page.locator(R+' button').first().click();await until(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='ready','ready before unknown');
  applyMode='unknown';await page.locator(C).click();await until(()=>document.querySelector('#fnVpnPickerV2Footer').dataset.state==='error','unknown result');assert.equal(await page.locator(C).isDisabled(),true);assert.equal(await page.locator('#fnVpnPickerV2Reset').isDisabled(),true);assert.equal(await page.locator(R+' button:not(:disabled)').count(),0,'unknown rollback STOP');await page.locator(C).dispatchEvent('click');assert.equal(countApply(),2,'no blind retry');assert.deepEqual(errors,[]);
  // Layout regression runs after existing VPN tests to avoid affecting their async topbar renders.
  await page.evaluate(()=>setPage('admin'));
  // Long SHA256/domain and one-time bearer must stay within Administration
  // card at mobile and desktop widths, without horizontal overflow.
  const originalOutputs=await page.evaluate(()=>{
    const ids=['adminCommandOutput','adminRouteOutput','connectorGrantOutput','connectorGrantSecret'];
    const original=ids.map(id=>{const n=document.getElementById(id);return{id,text:n.textContent,hidden:n.hidden}});
    document.getElementById('adminRouteOutput').textContent='Маршрут '+('a'.repeat(310))+'.example.com '+('f'.repeat(64));
    document.getElementById('connectorGrantOutput').textContent='Диагностика '+('long-token-'.repeat(38));
    const secret=document.getElementById('connectorGrantSecret');
    secret.textContent='Разовый токен: '+('a'.repeat(64));secret.hidden=false;
    return original;
  });
  for(const width of [320,375,768,1440]){
    await page.setViewportSize({width,height:900});
    const fit=await page.evaluate(()=>{
      const ids=['adminRouteOutput','connectorGrantOutput','connectorGrantSecret','adminCommandOutput'];
      return ids.map(id=>{
        const n=document.getElementById(id),r=n.getBoundingClientRect(),p=n.closest('.fn-admin-card').getBoundingClientRect();
        return{id,right:r.right,parentRight:p.right,scrollWidth:n.scrollWidth,clientWidth:n.clientWidth,whiteSpace:getComputedStyle(n).whiteSpace};
      });
    });
    for(const result of fit){
      assert.ok(result.right<=result.parentRight+1,'Admin '+result.id+' exceeds card at '+width+': '+JSON.stringify(result));
      assert.ok(result.scrollWidth<=result.clientWidth+2,'Admin '+result.id+' scrolls horizontally at '+width+': '+JSON.stringify(result));
      assert.equal(result.whiteSpace,'pre-wrap','Admin '+result.id+' must wrap diagnostics at '+width);
    }
  }
  await page.evaluate(original=>{
    for(const item of original){const n=document.getElementById(item.id);n.textContent=item.text;n.hidden=item.hidden;}
  },originalOutputs);
  await page.setViewportSize({width:1440,height:1000});
  await page.setViewportSize({width:1440,height:1000});
  console.log('PASS: actual production HTML/CSP, canonical flags, current identity, Ukraine exclusion, RTT refresh/sort, search, read-only check, exact apply, cache, errors/STOP, five viewports, navigation and liveness.');console.log('Other mocked GET surfaces: '+JSON.stringify([...new Set(unhandled)]));
})().catch(async error=>{
  console.error(error);
  if(page){try{fs.mkdirSync(artifacts,{recursive:true});await page.screenshot({path:path.join(artifacts,'failure.png')});console.error('BODY '+(await page.locator('body').innerText()).slice(0,2500));console.error('PAGE ERRORS '+JSON.stringify(errors));console.error('VPN REQUESTS '+JSON.stringify(calls.filter(c=>c.path.startsWith('/api/vpn/'))));console.error('ENGINE '+JSON.stringify(await page.evaluate(()=>({card:document.querySelector('#selectedProfileCard')?.outerHTML,button:document.querySelector('#exactConnectBtn')?.outerHTML,currentFlag:document.querySelector('#bestCurrentFlag')?.outerHTML}))));}catch(_){}}
  process.exitCode=1;
}).finally(async()=>{if(browser)await browser.close();fs.writeFileSync(addressFile+'.stop','stop');for(let i=0;i<40&&!serverExited;i++)await delay(100);if(!serverExited)server.kill('SIGTERM');if(serverLog.includes('FAIL'))console.error(serverLog)});
