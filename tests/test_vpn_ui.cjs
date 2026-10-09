// Shared-engine compatibility regression harness; production HTML/v2 is tested
// by test_control_center_production.cjs using the actual Go handlers.
// Cache/compatibility presentation must precede the coordinator, as in handleIndex.
// API fixtures use documentation-only addresses; no router or live VPN is contacted.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui/web');
const artifacts = process.env.FREENET_UI_ARTIFACTS || path.join(root, 'test-artifacts');
const current = {id:'fixture-pl', name:'Польша · Варшава', country_code:'pl', endpoint:'192.0.2.10:443', current:true, tested:true, eligible:true, available:true, reachable:true, download_mbps:42.5, throughput_source:'strict_aggregate', application_rtt_ms:140, tcp_rtt_ms:95, jitter_ms:8,media_samples:4,media_stalls:0,service_ok:4,service_total:4};
const winner = {...current,id:'fixture-de',name:'Германия · Франкфурт',country_code:'de',endpoint:'192.0.2.20:443',current:false,download_mbps:68.2,application_rtt_ms:110};
const second = {...winner,id:'fixture-lt',name:'Литва · Вильнюс',country_code:'lt',endpoint:'192.0.2.40:443',download_mbps:59.1};
const third = {...winner,id:'fixture-fi',name:'Финляндия · Хельсинки',country_code:'fi',endpoint:'192.0.2.50:443',download_mbps:31.4,application_rtt_ms:180};
const bestSelectionToken = '0123456789abcdef0123456789abcdef';
const manualRTTToken = 'abcdef0123456789abcdef0123456789';
let expectedApply = winner;
let status = {version:'0.2.88',country:'Польша',city:'Варшава',country_code:'pl',endpoint:current.endpoint,xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',setup_complete:true,install_scenario:'existing_stack',subscription_configured:true};
const server = http.createServer((req,res)=>{
  const url = new URL(req.url,'http://localhost');
  if(url.pathname==='/') {
    const html=fs.readFileSync(path.join(web,'index.html'),'utf8').replace('</body>', ['self-update.js','vpn-ux-fix.js','topbar-settings-profile-cache.js','operation-coordinator.js','xray-core-manager.js','vpn-picker-v2.js'].map(f=>`<script src="/${f}"></script>`).join('')+'</body>');
    res.setHeader('Content-Type','text/html; charset=utf-8');res.end(html);return;
  }
  if(['/self-update.js','/vpn-ux-fix.js','/operation-coordinator.js','/topbar-settings-profile-cache.js','/xray-core-manager.js','/vpn-picker-v2.js'].includes(url.pathname)){res.setHeader('Content-Type','application/javascript');res.end(fs.readFileSync(path.join(web,url.pathname.slice(1))));return;}
  res.writeHead(404);res.end();
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true});
  try {
    const page=await browser.newPage({viewport:{width:1440,height:1000}});
    await page.addInitScript(() => {
      const proto = Object.getPrototypeOf(globalThis.crypto);
      if (proto && Object.prototype.hasOwnProperty.call(proto, 'randomUUID')) {
        try { Object.defineProperty(proto, 'randomUUID', {value: undefined, configurable: true}); } catch (_) {}
      } else {
        try { Object.defineProperty(globalThis.crypto, 'randomUUID', {value: undefined, configurable: true}); } catch (_) {}
      }
    });
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    const calls=[];let pending=[];let mode='ok';let bestMode='winner';let rttMode='ok';
    let applyMode='ok', operation=null, operationReads=0, providerPlanMode='ok';
    const answer = (route,body,code=200)=>route.fulfill({status:code,contentType:'application/json',body:JSON.stringify(body)});
    await page.route('**/api/**',async route=>{
      const req=route.request(),url=new URL(req.url());
      calls.push({path:url.pathname,query:url.search,method:req.method(),body:req.postData()});
      if(url.pathname==='/api/auth/status')return answer(route,{configured:true,authenticated:true});
      if(url.pathname==='/api/status')return answer(route,status);
      if(url.pathname==='/api/xray/service')return answer(route,{success:true,version:'v26.9.9',current_version:'v26.9.9'});
      if(url.pathname==='/api/operation/state'){
        operationReads++;
        if(operationReads===1)return answer(route,{success:true,active:true,operation:{...operation,state:'running',result:''}});
        return answer(route,{success:true,active:false,operation});
      }
      if(url.pathname==='/api/provider-profile/plan'){
        const id=url.searchParams.get('profile_id');
        const selected=[current,winner,second].find(p=>p.id===id) || expectedApply;
        const providerPlan=providerPlanMode==='error'
          ?{success:false,profile_id:id,candidate_xray_valid:false,mutation:'NONE',error:'FreeNet получил неполный ответ проверки VPN-сервера.'}
          :{success:true,profile_id:id,candidate_xray_valid:true,mutation:'NONE',endpoint:selected.endpoint};
        return answer(route,providerPlan,providerPlan.success?200:409);
      }
      if(url.pathname==='/api/provider-profiles/rtt'){
        const winnerEndpoint=rttMode==='rotate'?'192.0.2.99:443':winner.endpoint;
        const safeProfile=(p,forcedEndpoint='')=>{
          const value=forcedEndpoint||p.endpoint;
          const split=value.lastIndexOf(':');
          return {id:p.id,name:p.name,country_code:p.country_code,address:value.slice(0,split),port:Number(value.slice(split+1))};
        };
        const catalog=[
          safeProfile(second),
          safeProfile(winner,winnerEndpoint),
          safeProfile(current)
        ];
        return answer(route,{
          success:true,cached:false,mutation:'NONE',fresh:true,catalog_source:'direct',selection_token:manualRTTToken,profiles:3,catalog,catalog_key:'fixture-catalog',checked:3,reachable:3,unknown:0,partial:false,
          results:[
            {profile_id:second.id,endpoint:second.endpoint,rtt_ms:92,jitter_ms:3,reachable:true,attempted:true,status:'reachable'},
            {profile_id:winner.id,endpoint:rttMode==='malformed'?'192.0.2.98:443':winnerEndpoint,rtt_ms:121,jitter_ms:4,reachable:true,attempted:true,status:'reachable'},
            {profile_id:current.id,endpoint:current.endpoint,rtt_ms:148,jitter_ms:5,reachable:true,attempted:true,status:'reachable'}
          ]
        });
      }
      if(url.pathname==='/api/network-profile/plan'){
        const hasProvider=url.searchParams.has('provider_profile_id');
        const providerPlan=!hasProvider?undefined:providerPlanMode==='error'
          ?{success:false,candidate_xray_valid:false,mutation:'NONE',error:'FreeNet получил неполный ответ проверки VPN-сервера.'}
          :{success:true,candidate_xray_valid:true,mutation:'NONE',endpoint:expectedApply.endpoint};
        return answer(route,{success:true,supported:true,active:true,provider_plan:providerPlan,extra_profiles:[{...current,address:'192.0.2.10',port:443},{...winner,address:'192.0.2.20',port:443},{...second,address:'192.0.2.40',port:443},{id:'fixture-ru',name:'Россия',country_code:'ru',address:'192.0.2.30',port:443}]});
      }
      if(url.pathname==='/api/vpn/current-quality'||url.pathname==='/api/vpn/best-foreign'){
        if(mode==='job') {
          const job={id:url.searchParams.get('id'),mode:url.pathname.endsWith('best-foreign')?'best':'current'};
          if(url.searchParams.get('job')==='start')return answer(route,{...job,state:'running',stage:'quality',completed:0,total:1},202);
          return answer(route,{...job,state:'completed',result:{success:true,candidates:[current],scanned_at:'2026-09-08T03:00:00Z'}},202);
        }
        pending.push(url.pathname);
        await new Promise(resolve=>setTimeout(resolve,350));
        pending.pop();
        if(mode==='network')return route.abort();
        if(mode==='http')return answer(route,{success:false,error:'Проверка временно недоступна'},503);
        if(mode==='malformed')return route.fulfill({status:200,contentType:'application/json',body:'invalid'});
        if(mode==='gateway')return route.fulfill({status:504,contentType:'text/html',body:'<h1>Gateway Timeout</h1>'});
        if(mode==='incomplete')return answer(route,{success:true});
        if(mode==='empty')return answer(route,{success:true,available:false,candidates:[],profiles_scanned:0});
        if(mode==='speed-missing' && url.pathname==='/api/vpn/current-quality') {
          const speedMissing={...current,eligible:false,download_mbps:0,throughput_source:'',download_issue:'Speedtest throughput unavailable',media_samples:0,media_stalls:0,media_grade:'unknown'};
          return answer(route,{success:true,available:false,candidates:[speedMissing],profiles_scanned:1,current_endpoint:current.endpoint,scanned_at:'2026-09-08T03:00:00Z'});
        }
        if(mode==='dead' && url.pathname==='/api/vpn/current-quality') {
          const deadCurrent={...current,tested:true,eligible:false,available:false,reachable:false,download_mbps:0,application_rtt_ms:0,tcp_rtt_ms:0,jitter_ms:0,reason:'Активный VPN-путь не подтвердил доступ к интернету'};
          return answer(route,{success:true,available:false,candidates:[deadCurrent],profiles_scanned:1,current_endpoint:current.endpoint,scanned_at:'2026-09-08T03:00:00Z'});
        }
        if(mode==='rejected')return answer(route,{success:true,available:false,candidates:[current,
          {...winner,tested:true,eligible:false,download_mbps:4.2,media_samples:4,media_stalls:0,service_ok:4,service_total:4,rejections:['Скорость ниже 20 Мбит/с']},
          {...third,tested:true,eligible:false,available:true,reachable:true,download_mbps:146,application_rtt_ms:226,media_samples:4,media_stalls:0,service_ok:4,service_total:4,rejections:['Отклик сайтов выше 220 мс']}],profiles_scanned:3});
        const best=url.pathname.endsWith('best-foreign');
        const recommendation=bestMode==='current'?current:bestMode==='ru'?{...winner,country_code:'ru'}:winner;
        const bestCandidates = bestMode==='ru'
          ? [current,{...winner,country_code:'ru'}]
          : bestMode==='no-current'
            ? [winner,second,third,{...third,id:'duplicate'}, {...winner,id:'unmeasured',endpoint:'192.0.2.60:443',media_samples:0}]
            : [current,winner,second,third,{...third,id:'duplicate'}, {...winner,id:'unmeasured',endpoint:'192.0.2.60:443',media_samples:0}];
        return answer(route,{success:true,available:true,candidates:best?bestCandidates:[current],recommendation:best?recommendation:null,selection_token:best?bestSelectionToken:undefined,profiles_scanned:best?6:1,current_endpoint:current.endpoint,scanned_at:'2026-09-08T03:00:00Z'});
      }
      if(url.pathname==='/api/network-profile/apply'){
        const payload=JSON.parse(req.postData());
        assert.equal(payload.operation,'provider');
        assert.equal(payload.profile_id,expectedApply.id);
        assert.equal(payload.confirm,true);
        if(Object.prototype.hasOwnProperty.call(payload,'selection_token'))assert.ok([bestSelectionToken,manualRTTToken].includes(payload.selection_token));
        status={...status,country:'Германия',city:'Франкфурт',country_code:'de',endpoint:expectedApply.endpoint};
        if(applyMode!=='ok'){
          status=expectedApply.id===second.id
            ? {...status,country:'',country_code:'',city:'',profile_label:expectedApply.name,endpoint:expectedApply.endpoint}
            : {...status,country:'Германия',country_code:'de',city:'Франкфурт',profile_label:expectedApply.name,endpoint:expectedApply.endpoint};
          const operationTarget=payload.selection_token?expectedApply.id+'@'+payload.selection_token:expectedApply.id;
          operation={id:'fixture-op',kind:'provider',target:operationTarget,started_at:new Date().toISOString(),state:'success',result:'SUCCESS'};
          if(applyMode==='failed')operation={...operation,state:'failed',result:'FAIL',error:'Проверка соединения не пройдена'};
          if(applyMode==='other')operation={...operation,target:'another-profile'};
          if(applyMode==='manual-ok')return answer(route,{success:true,applied:true});
          if(applyMode==='aborted')return route.abort();
          return route.fulfill({status:applyMode==='empty'?200:504,contentType:'text/html',body:applyMode==='empty'?'':'<h1>Gateway Timeout</h1>'});
        }
        return answer(route,{success:true,applied:true,provider_plan:{profile_id:expectedApply.id,endpoint:expectedApply.endpoint,candidate_xray_valid:true,candidate_route_ok:true,mutation:'APPLIED'}});
      }
      return answer(route,{success:true,available:false,configured:true,active:false});
    });
    const base=`http://127.0.0.1:${server.address().port}`;
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
    assert.equal(await page.evaluate(()=>typeof globalThis.crypto.randomUUID), 'undefined', 'browser fixture must reproduce insecure-LAN randomUUID absence');
    await page.waitForTimeout(150);
    // v0.3.8 regression guard: any ordinary DOM mutation must leave the browser event loop alive.
    await page.evaluate(() => {
      window.__freenetLiveness = 0;
      const probe = document.createElement('div');
      probe.id = 'fn-liveness-probe';
      document.body.appendChild(probe);
      for (let i = 0; i < 40; i++) {
        probe.textContent = `mutation-${i}`;
        probe.classList.toggle('tick', i % 2 === 0);
      }
      setTimeout(() => { window.__freenetLiveness = 1; probe.remove(); }, 60);
    });
    await page.waitForFunction(() => window.__freenetLiveness === 1, null, {timeout: 1500});
    const scans=()=>calls.filter(c=>c.path.startsWith('/api/vpn/')&&!c.query.includes('job=status'));
    async function openPicker() {
      const popover = page.locator('#fnVpnPickerV2Panel');
      if (await popover.isHidden()) await page.locator('#fnVpnPickerV2Toggle').click();
      await popover.waitFor({state:'visible'});
    }
    assert.equal(scans().length,0,'opening Overview must not scan');
    assert.equal(errors.length,0,errors.join('\n'));
    assert.equal(await page.locator('#bestServerShell').count(),1);
    assert.equal(await page.locator('#fnVpnPickerV2Toggle').isVisible(),true,'topbar must expose one stable VPN selector control');
    await page.waitForFunction(()=>document.querySelector('#overviewApprovedTop')?.dataset.vpnOrder==='xray-vpn-dns-freenet');
    const topbarOrder = await page.evaluate(() => {
      const summary=document.querySelector('#overviewApprovedTop');
      return Array.from(summary.children).map(node => node.matches('.fn-xray-topbar')?'xray':node.id==='fnVpnPickerV2Host'?'vpn':node.id==='topFreenetUpdate'?'freenet':/DNS/i.test(node.textContent||'')?'dns':'other').filter(x=>x!=='other');
    });
    assert.deepEqual(topbarOrder,['xray','vpn','dns','freenet'],'topbar order must be Xray -> VPN -> DNS -> FreeNet');
    await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2Country')?.textContent==='Польша');
    assert.equal(await page.locator('#fnVpnPickerV2Flag').evaluate(node=>node.classList.contains('flag-pl')),true,'VPN chip must expose current country flag');
    assert.equal(await page.locator('#fnVpnPickerV2Panel').isHidden(),true,'VPN selector popover must be closed by default');
    const topbarHeightClosed = await page.locator('.topbar').evaluate(node => Math.round(node.getBoundingClientRect().height));
    const rttCallsBeforeOpen=calls.filter(x=>x.path==='/api/provider-profiles/rtt').length;
    await openPicker();
    await page.locator('#fnVpnPickerV2Results').waitFor({state:'visible'});
    await page.waitForTimeout(200);
    assert.equal(calls.filter(x=>x.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen,'opening selector must be network-idle and never auto-start RTT');
    const initialRTT=await page.locator('#fnVpnPickerV2Results .fnv2-option .fnv2-rtt').evaluateAll(nodes=>nodes.map(n=>n.textContent||''));
    assert.ok(initialRTT.every(v=>v===''),'selector without explicit RTT refresh must not invent measurement state');
    await page.evaluate(()=>document.querySelector('#fnVpnPickerV2Refresh').click());
    await page.waitForTimeout(120);
    assert.equal(calls.filter(x=>x.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen,'synthetic refresh click must never start RTT');
    await page.locator('#fnVpnPickerV2Refresh').click();
    await page.waitForFunction(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled);
    await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2RTTState')?.textContent.includes('Список отсортирован'));
    let rttOrder=await page.locator('#fnVpnPickerV2Results .fnv2-option').evaluateAll(nodes=>nodes.map(n=>({id:n.dataset.profileId,rtt:n.querySelector('.fnv2-rtt')?.textContent||''})));
    assert.deepEqual(rttOrder.slice(0,3).map(x=>x.id),[second.id,winner.id,current.id],'explicit RTT refresh must sort measured profiles by canonical VPN RTT');
    assert.deepEqual(rttOrder.slice(0,3).map(x=>x.rtt),['92 мс','121 мс','148 мс'],'explicit RTT refresh must show RTT for every measured profile');
    let forcedRTTCalls=calls.filter(x=>x.path==='/api/provider-profiles/rtt'&&x.query.includes('refresh=1')).length;
    assert.equal(forcedRTTCalls,1,'one explicit refresh must start exactly one full-pool RTT sweep');
    await page.locator('#fnVpnPickerV2Close').click();
    await openPicker();
    await page.waitForTimeout(200);
    assert.equal(calls.filter(x=>x.path==='/api/provider-profiles/rtt').length,rttCallsBeforeOpen+1,'reopening selector must not start another RTT sweep');

    rttMode='rotate';
    await page.locator('#fnVpnPickerV2Refresh').click();
    await page.waitForFunction(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled);
    await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2RTTState')?.textContent.includes('Список отсортирован'));
    assert.doesNotMatch(await page.locator('#fnVpnPickerV2RTTState').textContent(),/Список VPN изменился/,'endpoint rotation before sweep is a valid new measured snapshot');
    assert.equal(await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-de"] small').textContent(),'192.0.2.99:443','selector must atomically adopt the endpoint snapshot that was actually measured');
    assert.equal(await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-de"] .fnv2-rtt').textContent(),'121 мс','rotated measured snapshot must retain its RTT');

    rttMode='malformed';
    await page.locator('#fnVpnPickerV2Refresh').click();
    await page.waitForFunction(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled);
    assert.match(await page.locator('#fnVpnPickerV2RTTState').textContent(),/Список VPN изменился/,'internally inconsistent RTT/catalog response must fail closed');
    const rejectedRTT=await page.locator('#fnVpnPickerV2Results .fnv2-option .fnv2-rtt').evaluateAll(nodes=>nodes.map(n=>n.textContent||''));
    assert.ok(rejectedRTT.every(v=>v===''),'inconsistent snapshot must not attach RTT values');

    rttMode='ok';
    await page.locator('#fnVpnPickerV2Refresh').click();
    await page.waitForFunction(()=>!document.querySelector('#fnVpnPickerV2Refresh').disabled);
    await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2RTTState')?.textContent.includes('Список отсортирован'));
    assert.equal(await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-de"] small').textContent(),winner.endpoint,'fresh baseline snapshot must restore canonical endpoint after rotation test');
    await page.waitForFunction(() => {
      const pop = document.querySelector('#fnVpnPickerV2Panel')?.getBoundingClientRect();
      const search = document.querySelector('#fnVpnPickerV2Search')?.getBoundingClientRect();
      const menu = document.querySelector('#fnVpnPickerV2Results')?.getBoundingClientRect();
      if (!pop || !search || !menu || pop.width <= 0) return false;
      return search.width > pop.width * 0.85 &&
        menu.width > pop.width * 0.85 &&
        document.querySelector('#fnVpnPickerV2Results').scrollWidth <= menu.width + 1;
    }, null, {timeout: 2000});
    const topbarHeightOpen = await page.locator('.topbar').evaluate(node => Math.round(node.getBoundingClientRect().height));
    const selectorGeometry = await page.evaluate(() => {
      const pop=document.querySelector('#fnVpnPickerV2Panel').getBoundingClientRect();
      const search=document.querySelector('#fnVpnPickerV2Search').getBoundingClientRect();
      const menu=document.querySelector('#fnVpnPickerV2Results').getBoundingClientRect();
      return {popWidth:pop.width,searchWidth:search.width,menuWidth:menu.width,menuScrollWidth:document.querySelector('#fnVpnPickerV2Results').scrollWidth};
    });
    assert.ok(selectorGeometry.searchWidth > selectorGeometry.popWidth*0.85, 'search must use modal width: '+JSON.stringify(selectorGeometry));
    assert.ok(selectorGeometry.menuWidth > selectorGeometry.popWidth*0.85, 'profile list must use modal width: '+JSON.stringify(selectorGeometry));
    assert.ok(selectorGeometry.menuScrollWidth <= selectorGeometry.menuWidth + 1, 'profile list must not have horizontal scroll: '+JSON.stringify(selectorGeometry));
    assert.equal(topbarHeightOpen, topbarHeightClosed, 'opening VPN selector must not change topbar height');
    assert.equal(await page.locator('#fnVpnPickerV2Panel').isVisible(),true,'v2 selector must own the visible popover while the exact engine stays hidden');
    await page.locator('#fnVpnPickerV2Search').fill('Литва');
    await page.waitForFunction(() => !document.querySelector('#fnVpnPickerV2Results').hidden);
    assert.equal(await page.locator('.topbar').evaluate(node => Math.round(node.getBoundingClientRect().height)), topbarHeightClosed, 'search/list state must not change topbar height');
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#fnVpnPickerV2Panel').isHidden(),true,'Escape must close VPN selector popover');
    assert.equal(await page.locator('#fnVpnPickerV2Search').inputValue(),'Литва','closing popover must not silently discard the user search');
    await openPicker();
    await page.locator('#fnVpnPickerV2Search').fill('');
    await page.keyboard.press('Escape');
    status={...status,country:'',city:'',country_code:'',profile_label:'BE Brussels, Belgium, Extra'};
    await page.evaluate(()=>loadStatus());
    await page.waitForFunction(()=>document.querySelector('#bestCurrentFlag')?.classList.contains('flag-be'));
    assert.equal(await page.locator('#bestCurrentFlag').isVisible(),true,'current VPN flag must be derived from exact profile label when status country_code is absent');
    status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
    await page.evaluate(()=>loadStatus());
    await page.waitForFunction(()=>document.querySelector('#bestCurrentFlag')?.classList.contains('flag-pl'));
    const overviewPolish = await page.evaluate(() => {
      const header = document.querySelector('.best-v4-current');
      const flag = document.querySelector('#bestCurrentFlag');
      const name = document.querySelector('#bestCurrentName');
      const badge = document.querySelector('.fn-current-connected');
      const setup = document.querySelector('#setupSummary');
      const fr = flag.getBoundingClientRect(), nr = name.getBoundingClientRect();
      return {
        headerDisplay: getComputedStyle(header).display,
        headerBackground: getComputedStyle(header).backgroundColor,
        flagRight: fr.right,
        nameLeft: nr.left,
        badgeMarginLeft: badge ? parseFloat(getComputedStyle(badge).marginLeft) : 0,
        setupRadius: setup ? parseFloat(getComputedStyle(setup).borderRadius) : 0,
        setupDisplay: setup ? getComputedStyle(setup).display : ''
      };
    });
    assert.equal(overviewPolish.headerDisplay, 'grid', `current VPN identity must use the unified grid header: ${JSON.stringify(overviewPolish)}`);
    assert.ok(overviewPolish.flagRight <= overviewPolish.nameLeft + 1, `flag must sit immediately before current VPN copy: ${JSON.stringify(overviewPolish)}`);
    assert.ok(overviewPolish.badgeMarginLeft >= 7, `connected badge needs breathing room: ${JSON.stringify(overviewPolish)}`);
    assert.ok(overviewPolish.setupRadius >= 14 && ['flex','inline-flex'].includes(overviewPolish.setupDisplay), `setup complete state must render as a status chip: ${JSON.stringify(overviewPolish)}`);
    fs.mkdirSync(artifacts,{recursive:true});
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-initial.png'),fullPage:true});

    // Persisted first-paint quality is rendered by a separate bootstrap script.
    // Its bridge must also hydrate the coordinator closure so a broad Best
    // Server remount cannot replace confirmed current metrics with dashes.
    await page.evaluate(candidate => {
      document.dispatchEvent(new CustomEvent('freenet:current-quality-display', {
        detail: {candidate, scanned_at:'2026-09-08T03:00:00Z'}
      }));
    }, current);
    bestMode='no-current';
    await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.match(await page.locator('#bestCurrentMetrics').textContent(),/42\.5 Мбит\/с/,'broad scan without a current candidate must preserve bridged current metrics');
    assert.match(await page.locator('#bestServerReason').textContent(),/Скорость \+25\.7/,'comparison deltas must still use the bridged current baseline');
    bestMode='winner';
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));

    async function currentCheck(){
      const before=scans().length;
      await page.locator('#bestServerCheckCurrent').click();
      assert.equal(await page.locator('#bestServerCheckCurrent').textContent(),'Проверяем текущий…');
      assert.equal(await page.locator('#bestServerRefresh').isDisabled(),true);
      assert.equal(await page.locator('#bestServerAdvanced').evaluate(n=>n.inert),true,'manual changes are blocked during scan');
      await page.evaluate(()=>{buttonsBusy(false);document.querySelector('#bestServerCheckCurrent').click();});
      assert.equal(await page.locator('#bestServerCheckCurrent').isDisabled(),true,'status polling must not enable a running scan');
      await page.waitForFunction(()=>!document.querySelector('#bestServerCheckCurrent').disabled);
      assert.equal(scans().length,before+1,'one click produces exactly one current-only request');
      assert.equal(scans().at(-1).path,'/api/vpn/current-quality');
    }
    await currentCheck();
    assert.match(await page.locator('#bestCurrentQuality').textContent(),/42.5/);
    assert.match(await page.locator('#bestServerStatus').textContent(),/завершена/);
    assert.equal(await page.locator('#bestServerResult').isVisible(),false);

    mode='speed-missing';
    await currentCheck();
    const missingSpeedMetrics=await page.locator('#bestCurrentMetrics').textContent();
    assert.match(missingSpeedMetrics,/Скорость VPN/,'current VPN keeps the canonical speed metric owner');
    assert.doesNotMatch(missingSpeedMetrics,/Мбит\/с/,'no alternative throughput value may replace a failed canonical measurement');
    assert.doesNotMatch(missingSpeedMetrics,/Быстрый|не для сравнения/i,'non-comparable speed UX must not return');
    assert.match(await page.locator('#bestCurrentQuality').textContent(),/Скорость не измерена:/,'failed canonical speed must remain explicit');

    bestMode='no-current';
    await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.doesNotMatch(await page.locator('#bestServerReason').textContent(),/Скорость [+-]/,'missing canonical current speed must not be used for candidate speed comparison');

    bestMode='winner';
    mode='dead';
    await currentCheck();
    assert.match(await page.locator('#bestCurrentHealth').textContent(),/VPN-путь не подтвердил доступ к интернету/);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentConnection')?.textContent.includes('VPN не работает'));
    assert.equal(await page.locator('#bestCurrentConnection').textContent(),'● VPN не работает');
    mode='ok';
    await currentCheck();
    assert.match(await page.locator('#bestCurrentMetrics').textContent(),/Скорость VPN/);
    assert.doesNotMatch(await page.locator('#bestCurrentMetrics').textContent(),/не для сравнения/);
    status={...status,xray_online:false};
    await page.evaluate(()=>loadStatus());
    await page.waitForFunction(()=>document.querySelector('#bestCurrentHealth')?.classList.contains('offline'));
    assert.match(await page.locator('#bestCurrentHealth').textContent(),/VPN сейчас не подключен/);
    assert.doesNotMatch(await page.locator('#bestCurrentHealth').textContent(),/работает стабильно/,'live offline state must override cached green quality');
    status={...status,xray_online:true};
    await page.evaluate(()=>loadStatus());
    await page.waitForFunction(()=>!document.querySelector('#bestCurrentHealth')?.classList.contains('offline'));
    mode='job';
    const jobsBefore=calls.length;
    await page.locator('#bestServerCheckCurrent').click();
    await page.locator('#fnQualityProgress').waitFor({state:'visible'});
    assert.match(await page.locator('#fnQualityProgress').textContent(),/Прошло.*с/);
    assert.equal(await page.locator('#controlCenter').evaluate(n=>n.inert),true);
    await page.waitForFunction(()=>!document.querySelector('#bestServerCheckCurrent').disabled);
    assert.equal(await page.locator('#fnQualityProgress').count(),0);
    assert.equal(await page.locator('#controlCenter').evaluate(n=>n.inert),false);
    const jobCalls=calls.slice(jobsBefore).filter(c=>c.path.startsWith('/api/vpn/'));
    assert.equal(jobCalls.filter(c=>c.query.includes('job=start')).length,1);
    assert.equal(jobCalls.filter(c=>c.query.includes('job=status')).length,1);
    const insecureHTTPJobID=new URLSearchParams(jobCalls.find(c=>c.query.includes('job=start')).query).get('id');
    assert.match(insecureHTTPJobID,/^[a-zA-Z0-9_-]{16,64}$/,'HTTP-LAN fallback quality job id must satisfy backend contract');
    mode='ok';
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-current.png'),fullPage:true});
    await page.evaluate(()=>{setPage('vpn');setPage('overview');const n=document.querySelector('#bestServerCheckCurrent');n.replaceWith(n.cloneNode(true));});
    await currentCheck();
    for(const failure of ['http','network','malformed']){
      mode=failure;await currentCheck();
      assert.match(await page.locator('#bestServerStatus').textContent(),/недоступна|Не удалось|не удалось/);
    }
    mode='empty';
    await currentCheck();
    assert.match(await page.locator('#bestServerStatus').textContent(),/завершена/,'a completed empty current scan may retain the last confirmed current-quality display');
    assert.match(await page.locator('#bestCurrentQuality').textContent(),/42.5/,'last confirmed current-quality is not erased by an empty identity response');
    mode='ok';
    for(const failure of ['http','network','malformed','gateway','incomplete']) {
      mode=failure;
      const before=scans().length;
      await page.locator('#bestServerRefresh').click();
      await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
      assert.equal(scans().length,before+1,'failed scan must not retry automatically');
      assert.match(await page.locator('#bestServerEmpty').textContent(),/пока неизвестно/);
      assert.doesNotMatch(await page.locator('#bestServerEmpty').textContent(),/вариантов.*нет/);
      const message=await page.locator('#bestServerStatus').textContent();
      assert.match(message,/Подбор не завершён/);
      if(failure==='gateway')assert.match(message,/HTTP 504/);
      if(failure==='malformed'||failure==='incomplete')assert.match(message,/HTTP 200/);
      assert.doesNotMatch(message,/<h1>/,'raw gateway response must not enter UI');
      assert.equal(await page.locator('.vpn-option-apply').count(),0);
      assert.equal(await page.locator('#bestServerAdvanced').evaluate(n=>n.inert),false);
    }
    mode='empty';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.match(await page.locator('#bestServerEmpty').textContent(),/Проверенных подходящих VPN сейчас нет/,'only a completed empty scan may report no eligible candidates');
    mode='rejected';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.equal(await page.locator('.vpn-rejected').count(),0,'rejected diagnostics must not fill final Top-3 cards');
    assert.equal(await page.locator('.vpn-warning').count(),0,'near-miss diagnostics must not fill final Top-3 cards');
    assert.equal(await page.locator('.vpn-option-apply').count(),0,'ineligible candidates must never become apply choices');
    assert.match(await page.locator('#bestServerEmpty').textContent(),/Проверенных подходящих VPN сейчас нет/);
    assert.doesNotMatch(await page.locator('#bestServerResult').textContent(),/Скорость ниже 20|Отклик сайтов выше 220 мс/,'rejected diagnostic reasons must not leak into final choice cards');
    await page.setViewportSize({width:1366,height:768});
    await page.screenshot({path:path.join(artifacts,'vpn-rejected-desktop.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollHeight<=innerHeight),true,'diagnostics fit desktop');
    await page.setViewportSize({width:390,height:844});
    await page.screenshot({path:path.join(artifacts,'vpn-rejected-mobile.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'diagnostics have no horizontal overflow');
    await page.setViewportSize({width:1440,height:1000});
    mode='ok';
    const beforeBest=scans().length;
    await page.locator('#bestServerRefresh').click();
    assert.equal(await page.locator('#bestServerRefresh').textContent(),'Подбираем…');
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.equal(scans().length,beforeBest+1);
    assert.equal(scans().at(-1).path,'/api/vpn/best-foreign');
    assert.equal(await page.locator('#bestServerApply').isVisible(),true);
    assert.match(await page.locator('#bestServerReason').textContent(),/Скорость \+25.7/);
    assert.match(await page.locator('.vpn-option').nth(2).textContent(),/Скорость −11.1.*Отклик медленнее на 40 мс/,'slower alternatives must disclose both drawbacks');
    assert.equal(calls.filter(c=>c.method==='POST').length,0,'checks never mutate VPN');
    assert.equal(await page.locator('.vpn-option').count(),3,'show up to three distinct measured foreign comparisons');
    assert.equal(await page.locator('#fnVpnPickerV2Toggle').isVisible(),true,'manual choice is always available through the stable topbar selector');
    assert.equal(await page.locator('#bestCurrentMetrics').isVisible(),true);
    for(const row of await page.locator('.vpn-option').all()) {
      assert.equal(await row.locator('.best-v4-metrics').isVisible(),true,'all comparison metrics visible without disclosure');
      assert.equal(await row.locator('.best-v4-pill').count(),4);
    }
    await page.setViewportSize({width:1366,height:768});
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-result.png'),fullPage:true});
    await openPicker();
    const pickerRect = await page.locator('#fnVpnPickerV2Panel').evaluate(node => {
      const r = node.getBoundingClientRect();
      return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:r.width,height:r.height,viewportWidth:innerWidth,viewportHeight:innerHeight};
    });
    assert.ok(pickerRect.left >= 0 && pickerRect.right <= pickerRect.viewportWidth + 1, `VPN selector popover escaped viewport horizontally: ${JSON.stringify(pickerRect)}`);
    assert.ok(pickerRect.bottom <= pickerRect.viewportHeight + 1, `VPN selector popover escaped viewport vertically: ${JSON.stringify(pickerRect)}`);
    await page.keyboard.press('Escape');
    const containment = await page.evaluate(() => {
      const panel = document.querySelector('.vpn-alternatives-panel').getBoundingClientRect();
      return [...document.querySelectorAll('#bestServerResult .vpn-option')].map(node => {
        const box = node.getBoundingClientRect();
        return {left:box.left,right:box.right,panelLeft:panel.left,panelRight:panel.right,scroll:node.scrollWidth,client:node.clientWidth};
      });
    });
    assert.ok(containment.length > 0, 'comparison cards missing');
    assert.ok(containment.every(x => x.left >= x.panelLeft - 1 && x.right <= x.panelRight + 1), `comparison card escaped alternatives pane: ${JSON.stringify(containment)}`);
    assert.ok(containment.every(x => x.scroll <= x.client + 1), `comparison card content overflowed its surface: ${JSON.stringify(containment)}`);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'desktop comparison and selector shell have no horizontal overflow');
    await page.setViewportSize({width:390,height:844});
    await page.screenshot({path:path.join(artifacts,'vpn-mobile-result.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'no horizontal overflow on mobile');
    assert.equal(await page.locator('.best-v4-pill:visible').evaluateAll(nodes=>nodes.every(n=>n.scrollWidth<=n.clientWidth)),true,'metric values must not overlap adjacent metrics on mobile');
    await openPicker();
    await page.locator('#fnVpnPickerV2Results').waitFor({state:'visible'});
    assert.doesNotMatch(await page.locator('#fnVpnPickerV2Results').textContent(),/Россия/);
    await page.locator('#fnVpnPickerV2Results').waitFor({state:'visible'});
    await page.keyboard.press('Escape');
    for(const kind of ['ru']){
      bestMode=kind;await page.locator('#bestServerRefresh').click();
      await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
      assert.equal(await page.locator('#bestServerApply').isVisible(),false,'RU recommendation cannot be applied');
    }
    bestMode='current';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.equal(await page.locator('.vpn-option').count(),3,'current winner still permits measured alternatives');
    assert.match(await page.locator('#bestServerStatus').textContent(),/Текущий VPN остаётся предпочтительным/);
    bestMode='winner';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    await page.locator('#bestServerApply').click();
    await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent==='VPN переключён. Соединение проверено.');
    assert.equal(calls.filter(c=>c.method==='POST').length,1,'apply issues one transactional request');
    assert.match(await page.locator('#bestCurrentName').textContent(),/Германия/);
    expectedApply=second;
    status={...status,endpoint:current.endpoint,country:'Польша',city:'Варшава',country_code:'pl'};
    await page.goto(base);
    await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    await page.locator('.vpn-option-apply').nth(1).click();
    await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent==='VPN переключён. Соединение проверено.');
    assert.equal(status.endpoint,second.endpoint);
    assert.equal(await page.locator('.vpn-option-apply').count(),0,'all stale alternatives cleared after switching');
    expectedApply=winner;
    for(const scenario of ['gateway','empty','aborted','failed','other']){
      applyMode=scenario;operationReads=0;
      status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
      await page.goto(base);
      await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
      await page.locator('#bestServerRefresh').click();
      await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
      const postsBefore=calls.filter(c=>c.method==='POST').length;
      await page.locator('#bestServerApply').click();
      if(scenario==='failed')await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent.includes('Проверка соединения не пройдена'));
      else if(scenario==='other')await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent.includes('пока не подтверждён'));
      else {
        await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent==='VPN переключён. Соединение проверено.');
        assert.match(await page.locator('#bestCurrentName').textContent(),/Германия|Germany/);
        assert.equal(await page.locator('#bestServerCheckCurrent').isDisabled(),false);
      }
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore+1,scenario+': no second POST');
      assert.ok(operationReads>=1,scenario+': operation state was read');
      assert.equal(await page.locator('#bestServerApply').isVisible(),false,scenario+': stale apply cannot be repeated');
    }
    expectedApply=second;providerPlanMode='error';applyMode='ok';operationReads=0;
    status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
    const providerErrorPosts=calls.filter(c=>c.method==='POST').length;
    const manualPlanReads=calls.filter(c=>c.path==='/api/provider-profile/plan').length;
    await openPicker();
    await page.locator('#fnVpnPickerV2Results').waitFor({state:'visible'});
    await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-lt"]').click();
    assert.equal(await page.locator('#fnVpnPickerV2Panel').isVisible(),true,'manual selection must stay inside open picker');
    await page.waitForFunction(()=>document.querySelector('#selectedProfileCard')?.classList.contains('is-ready'));
    assert.equal(await page.locator('#exactConnectBtn').isDisabled(),false,'dead/untested server must be selectable');
    assert.equal(calls.filter(c=>c.path==='/api/provider-profile/plan').length,manualPlanReads,'manual selection must not fetch provider plan');
    assert.equal(calls.filter(c=>c.method==='POST').length,providerErrorPosts,'selection is read-only');
    await page.setViewportSize({width:1366,height:768});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'desktop overflow');
    await page.setViewportSize({width:390,height:844});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'mobile overflow');
    await page.setViewportSize({width:1440,height:1000});
    providerPlanMode='ok';

    // Emergency manual choice: a successful explicit RTT sweep is enough to
    // enable Connect even while the subscription/plan route is unavailable.
    expectedApply=second;providerPlanMode='error';applyMode='manual-ok';operationReads=0;
    status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
    await openPicker();
    const planReadsBeforeRTT=calls.filter(c=>c.path==='/api/provider-profile/plan').length;
    await page.locator('#fnVpnPickerV2Refresh').click();
    await page.waitForFunction(()=>document.querySelector('#fnVpnPickerV2RTTState')?.textContent.includes('Список отсортирован'));
    assert.equal(await page.evaluate(()=>window.freenetManualRTTSelection?.token),manualRTTToken,
      'explicit RTT must retain a protected one-time selection token without credentials');
    await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-lt"]').click();
    await page.waitForFunction(()=>document.querySelector('#selectedProfileCard')?.classList.contains('is-ready'));
    assert.equal(calls.filter(c=>c.path==='/api/provider-profile/plan').length,planReadsBeforeRTT,
      'measured manual selection must never perform a redundant provider plan / subscription fetch');
    const emergencyPosts=calls.filter(c=>c.path==='/api/network-profile/apply').length;
    await page.locator('#fnVpnPickerV2Connect').click();
    await page.waitForFunction(()=>document.querySelector('#notice')?.textContent.includes('Конфигурация применена:'));
    const emergencyRequests=calls.filter(c=>c.path==='/api/network-profile/apply');
    assert.equal(emergencyRequests.length,emergencyPosts+1,'measured selection sends exactly one apply');
    assert.equal(JSON.parse(emergencyRequests.at(-1).body).manual_override,true,
      'manual apply must explicitly skip connectivity preflight');
    assert.equal(JSON.parse(emergencyRequests.at(-1).body).expected_endpoint,second.endpoint,
      'manual apply must bind expected endpoint');
    assert.equal('selection_token' in JSON.parse(emergencyRequests.at(-1).body),false,
      'manual apply must not require RTT selection token');
    assert.equal(calls.filter(c=>c.path==='/api/provider-profile/plan').length,planReadsBeforeRTT,
      'successful manual apply must not re-fetch subscription');
    providerPlanMode='ok';

    for(const scenario of ['manual-ok','gateway','other']) {
      expectedApply=second;applyMode=scenario;operationReads=0;
      status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
      await page.goto(base);
      await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
      const postsBefore=calls.filter(c=>c.method==='POST').length;
      await openPicker();
      await page.locator('#fnVpnPickerV2Results').waitFor({state:'visible'});
      await page.locator('#fnVpnPickerV2Results [data-profile-id="fixture-lt"]').click();
      assert.equal(await page.locator('#fnVpnPickerV2Panel').isVisible(),true,'exact candidate checking must remain inside the open popover');
      await page.waitForFunction(()=>!document.querySelector('#exactConnectBtn').disabled);
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore,'manual selection only validates');
      await page.locator('#fnVpnPickerV2Connect').click();
      if(scenario==='other')await page.waitForFunction(()=>document.querySelector('#notice').textContent.includes('пока не подтверждён'));
      else await page.waitForFunction(()=>document.querySelector('#notice').textContent.includes('Конфигурация применена:'));
      assert.equal(status.country_code,'','exact Extra acceptance must not depend on legacy country mapping');
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore+1);
      await page.evaluate(()=>{buttonsBusy(false);document.querySelector('#exactConnectBtn').click();});
      assert.equal(await page.locator('#exactConnectBtn').isDisabled(),true,'used manual plan cannot be re-enabled by polling');
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore+1,'no blind manual repeat');
    }
    assert.equal(errors.length,0,errors.join('\n'));
    console.log('PASS: shipped-page startup, explicit current/full scans, fresh-current retention, 3 comparison rows, CSS flags, RU exclusion, transactional apply reconciliation, exact-profile fallback and responsive layout.');
  } finally {await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
