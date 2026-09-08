// Browser regression tests for the shipped page and its real script load order.
// API fixtures use documentation-only addresses; no router or live VPN is contacted.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui/web');
const artifacts = process.env.FREENET_UI_ARTIFACTS || path.join(root, 'test-artifacts');
const current = {id:'fixture-pl', name:'Польша · Варшава', country_code:'pl', endpoint:'192.0.2.10:443', current:true, tested:true, eligible:true, available:true, reachable:true, download_mbps:42.5, application_rtt_ms:140, tcp_rtt_ms:95, jitter_ms:8,media_samples:4,media_stalls:0,service_ok:4,service_total:4};
const winner = {...current,id:'fixture-de',name:'Германия · Франкфурт',country_code:'de',endpoint:'192.0.2.20:443',current:false,download_mbps:68.2,application_rtt_ms:110};
const second = {...winner,id:'fixture-lt',name:'Литва · Вильнюс',country_code:'lt',endpoint:'192.0.2.40:443',download_mbps:59.1};
const third = {...winner,id:'fixture-fi',name:'Финляндия · Хельсинки',country_code:'fi',endpoint:'192.0.2.50:443',download_mbps:31.4,application_rtt_ms:180};
let expectedApply = winner;
let status = {version:'0.2.88',country:'Польша',city:'Варшава',country_code:'pl',endpoint:current.endpoint,xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'vladlink',isp_label:'Владлинк',setup_complete:true,install_scenario:'existing_stack',subscription_configured:true};
const server = http.createServer((req,res)=>{
  const url = new URL(req.url,'http://localhost');
  if(url.pathname==='/') {
    const html=fs.readFileSync(path.join(web,'index.html'),'utf8').replace('</body>', ['self-update.js','vpn-ux-fix.js','operation-coordinator.js'].map(f=>`<script src="/${f}"></script>`).join('')+'</body>');
    res.setHeader('Content-Type','text/html; charset=utf-8');res.end(html);return;
  }
  if(['/self-update.js','/vpn-ux-fix.js','/operation-coordinator.js'].includes(url.pathname)){res.setHeader('Content-Type','application/javascript');res.end(fs.readFileSync(path.join(web,url.pathname.slice(1))));return;}
  res.writeHead(404);res.end();
});
(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true});
  try {
    const page=await browser.newPage({viewport:{width:1440,height:1000}});
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    const calls=[];let pending=[];let mode='ok';let bestMode='winner';
    let applyMode='ok', operation=null, operationReads=0;
    const answer = (route,body,code=200)=>route.fulfill({status:code,contentType:'application/json',body:JSON.stringify(body)});
    await page.route('**/api/**',async route=>{
      const req=route.request(),url=new URL(req.url());
      calls.push({path:url.pathname,query:url.search,method:req.method(),body:req.postData()});
      if(url.pathname==='/api/auth/status')return answer(route,{configured:true,authenticated:true});
      if(url.pathname==='/api/status')return answer(route,status);
      if(url.pathname==='/api/operation/state'){
        operationReads++;
        if(operationReads===1)return answer(route,{success:true,active:true,operation:{...operation,state:'running',result:''}});
        return answer(route,{success:true,active:false,operation});
      }
      if(url.pathname==='/api/network-profile/plan')return answer(route,{success:true,supported:true,active:true,provider_plan:url.searchParams.has('provider_profile_id')?{success:true,candidate_xray_valid:true,mutation:'NONE',endpoint:expectedApply.endpoint}:undefined,extra_profiles:[{...current,address:'192.0.2.10',port:443},{...winner,address:'192.0.2.20',port:443},{...second,address:'192.0.2.40',port:443},{id:'fixture-ru',name:'Россия',country_code:'ru',address:'192.0.2.30',port:443}]});
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
        if(mode==='rejected')return answer(route,{success:true,available:false,candidates:[current,
          {...winner,tested:true,eligible:false,download_mbps:0,media_samples:0,rejections:['Скорость не измерена','Speedtest завершено 0/4'],media_issue:'4× HTTP 403; curl 0; получено 123 байт'},
          {...second,tested:true,eligible:false,download_mbps:4.2,rejections:['Speedtest ниже 20 Мбит/с']},
          {...third,tested:false,eligible:false}],profiles_scanned:4});
        const best=url.pathname.endsWith('best-foreign');
        const recommendation=bestMode==='current'?current:bestMode==='ru'?{...winner,country_code:'ru'}:winner;
        return answer(route,{success:true,available:true,candidates:best?(bestMode==='ru'?[current,{...winner,country_code:'ru'}]:[current,winner,second,third,{...third,id:'duplicate'}, {...winner,id:'unmeasured',endpoint:'192.0.2.60:443',media_samples:0}]):[current],recommendation:best?recommendation:null,profiles_scanned:best?6:1,scanned_at:'2026-09-08T03:00:00Z'});
      }
      if(url.pathname==='/api/network-profile/apply'){
        assert.deepEqual(JSON.parse(req.postData()),{operation:'provider',profile_id:expectedApply.id,confirm:true});
        status={...status,country:'Германия',city:'Франкфурт',country_code:'de',endpoint:expectedApply.endpoint};
        if(applyMode!=='ok'){
          status={...status,country:'',country_code:'',city:'',profile_label:'LT Lithuania, Extra Whitelist'};
          operation={id:'fixture-op',kind:'provider',target:expectedApply.id,started_at:new Date().toISOString(),state:'success',result:'SUCCESS'};
          if(applyMode==='failed')operation={...operation,state:'failed',result:'FAIL',error:'Проверка соединения не пройдена'};
          if(applyMode==='other')operation={...operation,target:'another-profile'};
          if(applyMode==='manual-ok')return answer(route,{success:true,applied:true});
          if(applyMode==='aborted')return route.abort();
          return route.fulfill({status:applyMode==='empty'?200:504,contentType:'text/html',body:applyMode==='empty'?'':'<h1>Gateway Timeout</h1>'});
        }
        return answer(route,{success:true,applied:true});
      }
      return answer(route,{success:true,available:false,configured:true,active:false});
    });
    const base=`http://127.0.0.1:${server.address().port}`;
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
    await page.waitForTimeout(150);
    const scans=()=>calls.filter(c=>c.path.startsWith('/api/vpn/')&&!c.query.includes('job=status'));
    assert.equal(scans().length,0,'opening Overview must not scan');
    assert.equal(errors.length,0,errors.join('\n'));
    assert.equal(await page.locator('#bestServerShell').count(),1);
    fs.mkdirSync(artifacts,{recursive:true});
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-initial.png'),fullPage:true});
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
    assert.match(await page.locator('#bestServerEmpty').textContent(),/вариантов для сравнения.*нет/,'only a completed empty scan may report no comparison candidates');
    mode='rejected';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.equal(await page.locator('.vpn-rejected').count(),2);
    assert.equal(await page.locator('.vpn-option-apply').count(),0,'rejected candidates have no suggested apply action');
    assert.match(await page.locator('#bestServerResult').textContent(),/Speedtest ниже 20/);
    assert.equal(await page.locator('.vpn-rejected .best-v4-pill.speed').count(),0,'untrusted speed is never green');
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
    assert.equal(await page.locator('#profilesTrigger').isVisible(),true,'manual choice is always open');
    assert.equal(await page.locator('#bestCurrentMetrics').isVisible(),true);
    for(const row of await page.locator('.vpn-option').all()) {
      assert.equal(await row.locator('.best-v4-metrics').isVisible(),true,'all comparison metrics visible without disclosure');
      assert.equal(await row.locator('.best-v4-pill').count(),4);
    }
    await page.setViewportSize({width:1366,height:768});
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-result.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.querySelector('#bestServerAdvanced').getBoundingClientRect().bottom <= innerHeight),true,'desktop comparison and manual controls fit viewport');
    await page.setViewportSize({width:390,height:844});
    await page.screenshot({path:path.join(artifacts,'vpn-mobile-result.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'no horizontal overflow on mobile');
    assert.equal(await page.locator('.best-v4-pill:visible').evaluateAll(nodes=>nodes.every(n=>n.scrollWidth<=n.clientWidth)),true,'metric values must not overlap adjacent metrics on mobile');
    await page.locator('#profilesTrigger').click();
    assert.doesNotMatch(await page.locator('#profilesMenu').textContent(),/Россия/);
    await page.locator('#profilesTrigger').click();
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
        assert.match(await page.locator('#bestCurrentName').textContent(),/Lithuania/);
        assert.equal(await page.locator('#bestServerCheckCurrent').isDisabled(),false);
      }
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore+1,scenario+': no second POST');
      assert.ok(operationReads>=1,scenario+': operation state was read');
      assert.equal(await page.locator('#bestServerApply').isVisible(),false,scenario+': stale apply cannot be repeated');
    }
    for(const scenario of ['manual-ok','gateway','other']) {
      expectedApply=second;applyMode=scenario;operationReads=0;
      status={...status,country:'Польша',city:'Варшава',country_code:'pl',profile_label:'',endpoint:current.endpoint};
      await page.goto(base);
      await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
      const postsBefore=calls.filter(c=>c.method==='POST').length;
      await page.locator('#profilesTrigger').click();
      await page.locator('[data-profile-id="fixture-lt"]').click();
      await page.waitForFunction(()=>!document.querySelector('#exactConnectBtn').disabled);
      assert.equal(calls.filter(c=>c.method==='POST').length,postsBefore,'manual selection only validates');
      await page.locator('#exactConnectBtn').click();
      if(scenario==='other')await page.waitForFunction(()=>document.querySelector('#notice').textContent.includes('пока не подтверждён'));
      else await page.waitForFunction(()=>document.querySelector('#notice').textContent.includes('Подключено:'));
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
