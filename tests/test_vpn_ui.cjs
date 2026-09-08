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
const current = {id:'fixture-pl', name:'Польша · Варшава', country_code:'pl', endpoint:'192.0.2.10:443', current:true, available:true, reachable:true, download_mbps:42.5, application_rtt_ms:140, tcp_rtt_ms:95, jitter_ms:8};
const winner = {...current,id:'fixture-de',name:'Германия · Франкфурт',country_code:'de',endpoint:'192.0.2.20:443',current:false,download_mbps:68.2,application_rtt_ms:110};
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
    const answer = (route,body,code=200)=>route.fulfill({status:code,contentType:'application/json',body:JSON.stringify(body)});
    await page.route('**/api/**',async route=>{
      const req=route.request(),url=new URL(req.url());
      calls.push({path:url.pathname,method:req.method(),body:req.postData()});
      if(url.pathname==='/api/auth/status')return answer(route,{configured:true,authenticated:true});
      if(url.pathname==='/api/status')return answer(route,status);
      if(url.pathname==='/api/network-profile/plan')return answer(route,{success:true,supported:true,active:true,extra_profiles:[{...current,address:'192.0.2.10',port:443},{...winner,address:'192.0.2.20',port:443},{id:'fixture-ru',name:'Россия',country_code:'ru',address:'192.0.2.30',port:443}]});
      if(url.pathname==='/api/vpn/current-quality'||url.pathname==='/api/vpn/best-foreign'){
        pending.push(url.pathname);
        await new Promise(resolve=>setTimeout(resolve,350));
        pending.pop();
        if(mode==='network')return route.abort();
        if(mode==='http')return answer(route,{success:false,error:'Проверка временно недоступна'},503);
        if(mode==='malformed')return route.fulfill({status:200,contentType:'application/json',body:'invalid'});
        if(mode==='empty')return answer(route,{success:true,available:false,candidates:[],profiles_scanned:0});
        const best=url.pathname.endsWith('best-foreign');
        const recommendation=bestMode==='current'?current:bestMode==='ru'?{...winner,country_code:'ru'}:winner;
        return answer(route,{success:true,available:true,candidates:best?[current,winner]:[current],recommendation:best?recommendation:null,profiles_scanned:best?2:1,scanned_at:'2026-09-08T03:00:00Z'});
      }
      if(url.pathname==='/api/network-profile/apply'){
        assert.deepEqual(JSON.parse(req.postData()),{operation:'provider',profile_id:winner.id,confirm:true});
        status={...status,country:'Германия',city:'Франкфурт',country_code:'de',endpoint:winner.endpoint};
        return answer(route,{success:true,applied:true});
      }
      return answer(route,{success:true,available:false,configured:true,active:false});
    });
    const base=`http://127.0.0.1:${server.address().port}`;
    await page.goto(base);
    await page.waitForFunction(()=>document.querySelector('#bestCurrentName').textContent.includes('Польша'));
    await page.waitForTimeout(150);
    const scans=()=>calls.filter(c=>c.path.startsWith('/api/vpn/'));
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
      await page.evaluate(()=>document.querySelector('#bestServerCheckCurrent').click());
      await page.waitForFunction(()=>!document.querySelector('#bestServerCheckCurrent').disabled);
      assert.equal(scans().length,before+1,'one click produces exactly one current-only request');
      assert.equal(scans().at(-1).path,'/api/vpn/current-quality');
    }
    await currentCheck();
    assert.match(await page.locator('#bestCurrentQuality').textContent(),/42.5/);
    assert.match(await page.locator('#bestServerStatus').textContent(),/завершена/);
    assert.equal(await page.locator('#bestServerResult').isVisible(),false);
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-current.png'),fullPage:true});
    // Return to Overview, then replace the button node: delegation must survive both.
    await page.evaluate(()=>{setPage('vpn');setPage('overview');const n=document.querySelector('#bestServerCheckCurrent');n.replaceWith(n.cloneNode(true));});
    await currentCheck();
    for(const failure of ['http','network','malformed','empty']){
      mode=failure;await currentCheck();
      assert.match(await page.locator('#bestServerStatus').textContent(),/недоступна|Не удалось|не удалось/);
    }
    mode='ok';
    const beforeBest=scans().length;
    await page.locator('#bestServerRefresh').click();
    assert.equal(await page.locator('#bestServerRefresh').textContent(),'Ищем лучший…');
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    assert.equal(scans().length,beforeBest+1);
    assert.equal(scans().at(-1).path,'/api/vpn/best-foreign');
    assert.equal(await page.locator('#bestServerApply').isVisible(),true);
    assert.match(await page.locator('#bestServerReason').textContent(),/скорость выше/);
    assert.equal(calls.filter(c=>c.method==='POST').length,0,'checks never mutate VPN');
    await page.screenshot({path:path.join(artifacts,'vpn-desktop-result.png'),fullPage:true});
    await page.setViewportSize({width:390,height:844});
    await page.screenshot({path:path.join(artifacts,'vpn-mobile-result.png'),fullPage:true});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'no horizontal overflow on mobile');
    await page.locator('#bestServerAdvanced > summary').click();
    await page.locator('#profilesTrigger').click();
    assert.doesNotMatch(await page.locator('#profilesMenu').textContent(),/Россия/);
    await page.locator('#bestServerAdvanced > summary').click();
    for(const kind of ['current','ru']){
      bestMode=kind;await page.locator('#bestServerRefresh').click();
      await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
      assert.equal(await page.locator('#bestServerApply').isVisible(),false,'current/RU recommendation cannot be applied');
    }
    bestMode='winner';await page.locator('#bestServerRefresh').click();
    await page.waitForFunction(()=>!document.querySelector('#bestServerRefresh').disabled);
    await page.locator('#bestServerApply').click();
    await page.waitForFunction(()=>document.querySelector('#bestServerStatus').textContent==='VPN переключён. Соединение проверено.');
    assert.equal(calls.filter(c=>c.method==='POST').length,1,'apply issues one transactional request');
    assert.match(await page.locator('#bestCurrentName').textContent(),/Германия/);
    assert.equal(errors.length,0,errors.join('\n'));
    console.log('PASS: shipped-page startup, no auto scan, current-only click/re-entry/remount/single-flight, HTTP/network/JSON/empty results, foreign scan, RU/manual exclusion, current-winner guard, exact apply, mobile overflow.');
  } finally {await browser.close();server.close();}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
