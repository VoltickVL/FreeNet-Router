// Browser regression for the production Routing v2 canonical mount/apply lifecycle.
// automation.js intentionally runs before Routing v2 to reproduce the historical
// network -> routing rename race. Navigation itself is exercised through the
// canonical sidebar button; Config Studio has a separate browser gate.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const root = path.resolve(__dirname, '..');
const web = path.join(root, 'freenet-ui', 'web');
const calls = [];
let routingLive = {routing:{domainStrategy:'AsIs',rules:[
  {type:'field',inboundTag:['socks-in'],outboundTag:'vless-reality'},
  {type:'field',inboundTag:['direct-in'],outboundTag:'direct'},
  {type:'field',port:'53',outboundTag:'dns-out'},
  {type:'field',domain:[
    'ext:geosite.dat:category-ru','ext:geosite.dat:ru-available-only-inside','ext:geosite.dat:category-remote-control',
    'ext:geosite.dat:alibaba','ext:geosite.dat:tencent','ext:geosite.dat:apple','ext:geosite.dat:microsoft',
    'ext:geosite.dat:google','ext:geosite.dat:steam','ext:geosite.dat:ea','ext:geosite.dat:github',
    'ext:geosite.dat:xiaomi','ext:geosite.dat:nvidia','ext:geosite.dat:airchina','ext:geosite.dat:epic',
    'ext:geosite.dat:private','domain:4pda.to','domain:binance.com','domain:petzl.com','domain:netcraze.pro'
  ],outboundTag:'direct'},
  {type:'field',ip:['ext:geoip.dat:private','ext:geoip.dat:google','ext:geoip.dat:ru-whitelist'],outboundTag:'direct'},
  {type:'field',domain:['ext:geosite.dat:ru-blocked'],outboundTag:'vless-reality'},
  {type:'field',ip:['ext:geoip.dat:ru-blocked','ext:geoip.dat:ru-blocked-community','ext:geoip.dat:re-filter'],outboundTag:'vless-reality'},
  {type:'field',network:'tcp,udp',outboundTag:'vless-reality'}
]}};
let policyLive = {policy:{}};

function canonicalRoutingV2Source() {
  return fs.readFileSync(path.join(web, 'routing-v2.js'), 'utf8')
    .replaceAll('`04_outbounds.json`', '04_outbounds.json')
    .replace('[data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy', ':is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy')
    .replace("const page = qs('[data-page-view=\"network\"]');", "const page = qs('[data-page-view=\"routing\"],[data-page-view=\"network\"]');");
}
function json(res, body, code=200) { res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'}); res.end(JSON.stringify(body)); }
function bodyJSON(req) { return new Promise(resolve => { let raw=''; req.on('data', c => raw += c); req.on('end', () => { try { resolve(JSON.parse(raw||'{}')); } catch (_) { resolve({}); } }); }); }
const status={version:'0.3.69',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',profile_label:'Франкфурт-на-Майне, Германия, Extra',endpoint:'192.0.2.67:443',xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',isp:'custom',isp_label:'Свой',recommended_dns_mode:'xkeen',setup_complete:true,subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}};
const scripts=['vpn-ux-fix.js','automation.js','routing-v2-canonical.js','routing-apply-ui.js'];

const server=http.createServer(async(req,res)=>{
  const url=new URL(req.url,'http://localhost'); calls.push(`${req.method} ${url.pathname}${url.search}`);
  if(url.pathname==='/'){
    const html=fs.readFileSync(path.join(web,'index.html'),'utf8').replace('</body>',scripts.map(n=>`<script src="/${n}"></script>`).join('')+'</body>');
    res.writeHead(200,{'Content-Type':'text/html; charset=utf-8'});res.end(html);return;
  }
  if(url.pathname==='/routing-v2-canonical.js'){res.writeHead(200,{'Content-Type':'application/javascript'});res.end(canonicalRoutingV2Source());return;}
  if(scripts.some(n=>n!=='routing-v2-canonical.js'&&url.pathname===`/${n}`)){res.writeHead(200,{'Content-Type':'application/javascript'});res.end(fs.readFileSync(path.join(web,url.pathname.slice(1)),'utf8'));return;}
  if(url.pathname==='/api/auth/status')return json(res,{configured:true,authenticated:true});
  if(url.pathname==='/api/status')return json(res,status);
  if(url.pathname==='/api/network-profile/plan')return json(res,{success:true,supported:true,active:true,extra_profiles:[]});
  if(url.pathname==='/api/geodata/files')return json(res,{success:true,files:[{name:'geosite.dat',kind:'geosite',size:4096}],search_enabled:true});
  if(url.pathname==='/api/geodata/search')return json(res,{success:true,kind:'geosite',query:url.searchParams.get('q')||'',mutation:'NONE',matches:[{file:'geosite.dat',kind:'geosite',categories:['youtube'],truncated:true}],warnings:['broken.dat: geodata file is unreadable or invalid']});
  if(url.pathname==='/api/capabilities')return json(res,{success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if(url.pathname==='/api/subscription')return json(res,{success:true,configured:true});
  if(url.pathname==='/api/policy/compile'&&req.method==='POST'){
    const request=await bodyJSON(req);
    const rules=Array.isArray(request.rules)?request.rules:[];
    const compiledRules=rules.map((rule,index)=>{
      const action=String(rule.action||'DIRECT').toUpperCase();
      const kind=String(rule.selector?.kind||'');
      const payload_outbound=action==='DIRECT'?'direct':action==='VPN'?'vless-reality':'block';
      const dns_leg=(kind==='domain'||kind==='geosite')?(action==='DIRECT'?'dns-direct':action==='VPN'?'dns-vless':'block'):'';
      return {order:index,selector:rule.selector,action,payload_outbound,...(dns_leg?{dns_leg}:{})};
    });
    return json(res,{success:true,mutation:'NONE',compiled:{rules:compiledRules,payload:compiledRules,dns:compiledRules.filter(rule=>rule.dns_leg)}});
  }
  if(url.pathname==='/api/routing/config')return json(res,{success:true,mutation:'NONE',routing:routingLive,policy:policyLive,routing_present:true,policy_present:true,routing_sha256:'1'.repeat(64),policy_sha256:'2'.repeat(64)});
  if(url.pathname==='/api/routing/validate'&&req.method==='POST'){const c=await bodyJSON(req);return json(res,{success:true,mutation:'NONE',xray_valid:true,routing:c.routing,policy:c.policy});}
  if(url.pathname==='/api/routing/apply'&&req.method==='POST'){const c=await bodyJSON(req);routingLive=c.routing;policyLive=c.policy;return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',before:{'05_routing.json':'1111','06_policy.json':'2222'},after:{'05_routing.json':'3333','06_policy.json':'2222'},result:'routing policy applied to managed sections'});}
  return json(res,{success:true});
});

(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true});
  try{
    const page=await browser.newPage({viewport:{width:1600,height:1000}});
    const errors=[];const consoleErrors=[];
    page.on('pageerror',e=>errors.push(e.message));
    page.on('console',m=>{if(m.type()==='error')consoleErrors.push(m.text());});
    page.on('dialog',d=>d.accept());
    const base=`http://127.0.0.1:${server.address().port}`;
    await page.goto(`${base}/`);

    // Mount must survive automation renaming network -> routing even before the user opens it.
    await page.waitForSelector('#routingV2Workspace',{state:'attached',timeout:10000});
    const pre=await page.evaluate(()=>({
      route:document.querySelector('#routingV2Workspace')?.closest('[data-page-view]')?.dataset.pageView||'',
      workspace:document.querySelectorAll('#routingV2Workspace').length,
      oldPreview:document.querySelectorAll('#policyBuilderPreview').length,
      applyCount:document.querySelectorAll('#rv2ApplyConfig').length,
      rulesApplyCount:document.querySelectorAll('#rv2ApplyRules').length
    }));
    assert.equal(pre.route,'routing',JSON.stringify(pre));
    assert.equal(pre.workspace,1,JSON.stringify(pre));
    assert.equal(pre.oldPreview,0,JSON.stringify(pre));
    assert.equal(pre.applyCount,1,JSON.stringify(pre));
    assert.equal(pre.rulesApplyCount,1,JSON.stringify(pre));

    const nav=page.locator('.nav-btn[data-page="routing"]');
    await nav.waitFor({state:'visible'});
    await nav.click();
    await page.waitForSelector('#routingV2Workspace',{state:'visible'});
    await page.waitForFunction(()=>document.querySelector('[data-page-view="routing"]')?.classList.contains('active'));
    assert.match(await page.locator('[data-page-view="routing"] .page-head').textContent(),/GeoData-групп/);

    // The Rules tab must explain and visualize the actual live 05_routing state.
    await page.waitForFunction(()=>document.querySelector('#rv2LiveState')?.textContent.includes('8 активных'));
    assert.equal(await page.locator('#rv2LiveRuleList .rv2-live-rule').count(),8,'all real-like live routing rules must be visible');
    const liveText=await page.locator('#rv2LiveRuleList').innerText();
    assert.match(liveText,/GeoSite/);
    assert.match(liveText,/GeoIP/);
    assert.match(liveText,/DIRECT/);
    assert.match(liveText,/VPN/);
    assert.match(liveText,/DNS/);
    assert.match(liveText,/Системное правило/);
    assert.match(liveText,/защищено/);
    assert.doesNotMatch(liveText,/05_routing\.json/);
    assert.doesNotMatch(liveText,/inboundTag/);
    assert.doesNotMatch(liveText,/dns-out/);
    assert.match(liveText,/Ещё 14/);
    assert.equal(await page.locator('#rv2LiveRuleList .rv2-live-title strong', {hasText:'Обычное правило'}).count(),0);
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,0,'live visualization must be read-only');

    // Technical Xray labels stay hidden until the user explicitly asks for details.
    const firstSystem=page.locator('#rv2LiveRuleList .rv2-live-rule').first();
    assert.match(await firstSystem.innerText(),/входящее подключение/);
    assert.doesNotMatch(await firstSystem.innerText(),/inboundTag/);
    await firstSystem.locator('.rv2-live-details button').click();
    assert.match(await firstSystem.innerText(),/inboundTag/);

    // Large selector sets are compact by default and can be expanded locally.
    const largeRule=page.locator('#rv2LiveRuleList .rv2-live-rule').nth(3);
    assert.equal(await largeRule.locator('.rv2-selector-chip:visible').count(),6);
    await largeRule.locator('.rv2-selector-more').click();
    assert.equal(await largeRule.locator('.rv2-selector-chip:visible').count(),20);
    assert.match(await largeRule.innerText(),/Свернуть/);

    // Bounded GeoData search is read-only and surfaces partial-file warnings.
    await page.locator('#rv2Kind').selectOption('geosite');
    await page.locator('#rv2Value').fill('youtube');
    await page.locator('#rv2GeoSearch').click();
    await page.waitForSelector('.rv2-search-result');
    assert.match(await page.locator('.rv2-search-result').first().textContent(),/geosite:youtube/);
    assert.match(await page.locator('.rv2-search-result').first().textContent(),/ограничен безопасным лимитом/);
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/Часть GeoData недоступна/);
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/broken\.dat/);
    const geoMutationCalls = calls.filter(x => /^POST \/api\/(routing|action|network)/.test(x)).length;
    assert.equal(geoMutationCalls,0,'GeoData search must not mutate runtime state');
    await page.locator('.rv2-search-result').first().click();
    assert.equal(await page.locator('#rv2Value').inputValue(),'youtube');
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/Добавить правило/);

    // Empty draft must not present actionable validation/apply buttons.
    assert.equal(await page.locator('#rv2ValidateRules').isDisabled(),true);
    assert.equal(await page.locator('#rv2ApplyRules').isDisabled(),true);
    assert.equal(await page.locator('#rv2BuildConfig').isDisabled(),true);

    // Add a GeoSite rule without touching live state, validate it, then use the shared transactional apply engine.
    await page.locator('#rv2AddRule').click();
    await page.waitForFunction(()=>document.querySelectorAll('#rv2RuleList .rv2-rule').length===1);
    assert.match(await page.locator('#rv2RuleList').innerText(),/GeoSite · youtube/);
    assert.equal(await page.locator('#rv2LiveRuleList .rv2-live-rule').count(),8,'draft must not rewrite live visualization before apply');
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,0,'adding a rule must remain read-only');

    await page.locator('#rv2ValidateRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);
    assert.match(await page.locator('#rv2RulesApplyPreview').textContent(),/Существующие правила сохранены/);
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/Проверка пройдена/);

    const visualBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2RulesApplyResult')?.textContent.includes('APPLIED'),null,{timeout:10000});
    const visualAfter=calls.filter(x=>x==='POST /api/routing/apply').length;
    assert.equal(visualAfter,visualBefore+1,'visual rules apply must issue exactly one transactional mutation');
    assert.equal(routingLive.routing.rules.length,9,'new rule must be prepended while all existing live rules are preserved');
    assert.deepEqual(routingLive.routing.rules[0],{type:'field',outboundTag:'direct',domain:['geosite:youtube']});
    assert.deepEqual(routingLive.routing.rules[8],{type:'field',network:'tcp,udp',outboundTag:'vless-reality'},'complex existing rule must be preserved exact');

    // Reload returns to Overview; reopen Routing and confirm the applied rule is now live.
    await page.waitForSelector('#routingV2Workspace',{state:'attached'});
    const navAfterApply=page.locator('.nav-btn[data-page="routing"]');
    await navAfterApply.click();
    await page.waitForSelector('#routingV2Workspace',{state:'visible'});
    await page.waitForFunction(()=>document.querySelector('#rv2LiveState')?.textContent.includes('9 активных'));
    assert.match(await page.locator('#rv2LiveRuleList').innerText(),/youtube/);

    await page.locator('.rv2-mode[data-mode="config"]').click();
    await page.waitForSelector('#rv2ConfigEditor',{state:'visible'});
    await page.waitForFunction(()=>document.querySelector('#rv2ConfigState')?.textContent.includes('Live'));
    const candidate={routing:{domainStrategy:'AsIs',rules:[{type:'field',domain:['domain:example.test'],outboundTag:'direct'}]}};
    await page.locator('#rv2ConfigEditor').fill(JSON.stringify(candidate,null,2));
    await page.locator('#rv2ValidateConfig').click();
    await page.waitForFunction(()=>!document.querySelector('#rv2ApplyConfig')?.disabled);
    const preview=await page.locator('#rv2ApplyPreview').textContent();
    assert.match(preview,/05_routing\.json: будет изменён/);
    assert.match(preview,/06_policy\.json: без изменений/);

    const before=calls.filter(x=>x==='POST /api/routing/apply').length;
    await page.locator('#rv2ApplyConfig').click();
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyResult')?.textContent.includes('APPLIED'));
    const after=calls.filter(x=>x==='POST /api/routing/apply').length;
    assert.equal(after,before+1,'controlled apply must issue exactly one routing mutation');
    const result=await page.locator('#rv2ApplyResult').textContent();
    assert.match(result,/Результат: APPLIED/);assert.match(result,/Откат: NOT_NEEDED/);
    assert.equal(errors.length,0,errors.join('\n'));assert.equal(consoleErrors.length,0,consoleErrors.join('\n'));
    console.log('ROUTING_V2_RUNTIME',JSON.stringify(pre));console.log('ROUTING_V2_APPLY_CALLS',after);
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;});
