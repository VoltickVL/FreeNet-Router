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
let xrayOnline = true;
let xrayVersion = 'v26.9.9';
const geoSuggestRequests = [];
let xrayEvents = [{at:'2026-10-04T01:00:00Z',kind:'xray',result:'success',message:'Xray запущен через FreeNet.'}];
const xrayActions = [];
let xrayCatalogGets = 0;

function canonicalRoutingV2Source() {
  return fs.readFileSync(path.join(web, 'routing-v2.js'), 'utf8')
    .replaceAll('`04_outbounds.json`', '04_outbounds.json')
    .replace('[data-page-view="network"].fn-routing-v2>.card.fn-routing-v2-legacy', ':is([data-page-view="routing"],[data-page-view="network"]).fn-routing-v2>.card.fn-routing-v2-legacy')
    .replace("const page = qs('[data-page-view=\"network\"]');", "const page = qs('[data-page-view=\"routing\"],[data-page-view=\"network\"]');");
}
function json(res, body, code=200) { res.writeHead(code, {'Content-Type':'application/json; charset=utf-8'}); res.end(JSON.stringify(body)); }
function bodyJSON(req) { return new Promise(resolve => { let raw=''; req.on('data', c => raw += c); req.on('end', () => { try { resolve(JSON.parse(raw||'{}')); } catch (_) { resolve({}); } }); }); }
const status={version:'0.3.69',country:'Германия',city:'Франкфурт-на-Майне',country_code:'de',profile_label:'Франкфурт-на-Майне, Германия, Extra',endpoint:'192.0.2.67:443',xray_online:true,xkeen_ui_online:true,dns_out_present:true,dns_mode:'xkeen',setup_complete:true,subscription_configured:true,busy:false,updater_busy:false,last_action:{success:true}};
const scripts=['vpn-ux-fix.js','automation.js','xray-core-manager.js','routing-v2-canonical.js','routing-apply-ui.js'];

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
  if(url.pathname==='/api/geodata/files')return json(res,{success:true,files:[{name:'geosite.dat',kind:'geosite',size:4096},{name:'geoip.dat',kind:'geoip',size:4096},{name:'geosite-extra.dat',kind:'geosite',size:2048}],search_enabled:true});
  if(url.pathname==='/api/geodata/search')return json(res,{success:true,kind:'geosite',query:url.searchParams.get('q')||'',mutation:'NONE',matches:[{file:'geosite.dat',kind:'geosite',categories:['youtube'],truncated:true}],warnings:['broken.dat: geodata file is unreadable or invalid']});
  if(url.pathname==='/api/geodata/suggest'){
    const kind=url.searchParams.get('kind')||'';
    const q=(url.searchParams.get('q')||'').toLowerCase();
    const file=url.searchParams.get('file')||'';
    const mode=url.searchParams.get('mode')||'';
    geoSuggestRequests.push({kind,q,file,mode});
    if(kind==='geoip'&&q.includes('example.com')) return json(res,{success:true,kind,query:q,mode:'dns',mutation:'NONE',resolved:['1.1.1.7','8.8.8.8'],suggestions:[
      {file:'geoip.dat',kind:'geoip',category:'cloudflare',selector:'geoip:cloudflare',ext_selector:'ext:geoip.dat:cloudflare',match:'dns',evidence:['1.1.1.7']},
      {file:'geoip.dat',kind:'geoip',category:'google',selector:'geoip:google',ext_selector:'ext:geoip.dat:google',match:'dns',evidence:['8.8.8.8']}
    ],warnings:[]});
    if(kind==='geosite'&&file==='geosite-extra.dat') return json(res,{success:true,kind,query:q,mode:'prefix',mutation:'NONE',suggestions:[
      {file:'geosite-extra.dat',kind:'geosite',category:'youtube-extra',selector:'ext:geosite-extra.dat:youtube-extra',ext_selector:'ext:geosite-extra.dat:youtube-extra',match:'category'}
    ],warnings:[]});
    if(kind==='geosite'&&q==='steam.') return json(res,{success:false,kind,query:q,mutation:'NONE',error:'invalid host or URL'});
    if(kind==='geosite'&&q==='slow-blur'){
      await new Promise(resolve=>setTimeout(resolve,350));
      return json(res,{success:true,kind,query:q,mode:'prefix',mutation:'NONE',suggestions:[
        {file:'geosite.dat',kind:'geosite',category:'slow-blur-result',selector:'geosite:slow-blur-result',ext_selector:'ext:geosite.dat:slow-blur-result',match:'category'}
      ],warnings:[]});
    }
    if(kind==='geosite'&&q==='many') return json(res,{success:true,kind,query:q,mode:'prefix',mutation:'NONE',suggestions:Array.from({length:24},(_,index)=>({
      file:'geosite.dat',kind:'geosite',category:`many-${String(index+1).padStart(2,'0')}`,selector:`geosite:many-${String(index+1).padStart(2,'0')}`,ext_selector:`ext:geosite.dat:many-${String(index+1).padStart(2,'0')}`,match:'category'
    })),warnings:[]});
    if(kind==='geosite') return json(res,{success:true,kind,query:q,mode:q.includes('.')?'domain':'prefix',mutation:'NONE',suggestions:[
      {file:'geosite.dat',kind:'geosite',category:'youtube',selector:'geosite:youtube',ext_selector:'ext:geosite.dat:youtube',match:'category'}
    ],warnings:[]});
    if(kind==='geoip') return json(res,{success:true,kind,query:q,mode:'prefix',mutation:'NONE',suggestions:[
      {file:'geoip.dat',kind:'geoip',category:'private',selector:'geoip:private',ext_selector:'ext:geoip.dat:private',match:'category'}
    ],warnings:[]});
    return json(res,{success:false,mutation:'NONE',error:'invalid suggestion query'},400);
  }
  if(url.pathname==='/api/capabilities')return json(res,{success:true,split_dns_supported:true,memory_total_mib:1024,split_dns_min_mib:768});
  if(url.pathname==='/api/subscription')return json(res,{success:true,configured:true});
  if(url.pathname==='/api/xray/service'&&req.method==='GET')return json(res,{success:true,online:xrayOnline,version:xrayVersion,events:xrayEvents});
  if(url.pathname==='/api/xray/service'&&req.method==='POST'){
    const request=await bodyJSON(req);
    xrayActions.push(String(request.action||''));
    if(request.action==='stop') xrayOnline=false;
    else if(request.action==='start'||request.action==='restart') xrayOnline=true;
    xrayEvents=[{at:'2026-10-04T01:01:00Z',kind:'xray',result:'success',message:request.action==='stop'?'Xray остановлен через FreeNet.':request.action==='restart'?'Xray перезапущен через FreeNet.':'Xray запущен через FreeNet.'},...xrayEvents];
    return json(res,{success:true,online:xrayOnline,version:xrayVersion,message:xrayEvents[0].message,events:xrayEvents});
  }
  if(url.pathname==='/api/xray/core/catalog'){
    xrayCatalogGets++;
    return json(res,{success:true,current_version:xrayVersion,latest_version:'v26.10.1',releases:[
      {version:'v26.10.1',latest:true,current:false,prerelease:false,published_at:'2026-10-03T00:00:00Z'},
      {version:xrayVersion,latest:false,current:true,prerelease:false,published_at:'2026-09-20T00:00:00Z'}
    ]});
  }
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
  if(url.pathname==='/api/routing/apply'&&req.method==='POST'){const c=await bodyJSON(req);routingLive=c.routing;policyLive=c.policy;return json(res,{success:true,mutation:'APPLIED',xray_valid:true,applied:true,rollback:'NOT_NEEDED',core_restart:true,before:{'05_routing.json':'1111','06_policy.json':'2222'},after:{'05_routing.json':'3333','06_policy.json':'2222'},result:'routing policy applied; active Xray Core restarted with firewall-preserving core-only path'});}
  return json(res,{success:true});
});

(async()=>{
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const browser=await chromium.launch({headless:true});
  try{
    const page=await browser.newPage({viewport:{width:1600,height:1000}});
    const errors=[];const consoleErrors=[];let dialogCount=0;
    page.on('pageerror',e=>errors.push(e.message));
    page.on('console',m=>{if(m.type()==='error')consoleErrors.push(m.text());});
    page.on('dialog',d=>{dialogCount++;d.accept();});
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
    assert.match(await page.locator('[data-page-view="routing"] .page-head').textContent(),/Сайты и категории/);

    // Primary workspace order is Xray / Rules / Configuration; Xray is a first-class surface, not a Config Studio sub-control.
    const modeLabels = await page.locator('.rv2-mode').allTextContents();
    assert.deepEqual(modeLabels.map(v=>v.trim()), ['Xray','Правила','Конфигурация']);
    assert.equal(await page.locator('.rv2-mode[data-mode="rules"]').evaluate(el=>el.classList.contains('active')),true,'Rules remains the default routing workspace');
    await page.locator('.rv2-mode[data-mode="xray"]').click();
    await page.waitForSelector('#rv2XrayPanel',{state:'visible'});
    await page.waitForFunction(()=>document.querySelector('#rv2XrayStatus')?.textContent==='Работает');
    assert.equal(await page.locator('#rv2XrayVersion').textContent(),'v26.9.9');
    assert.match(await page.locator('#rv2XrayEvents').innerText(),/Xray запущен через FreeNet/);
    assert.equal(await page.locator('#rv2XrayJournal').count(),0,'embedded Xray surface must not duplicate the full Journal action');
    assert.equal(await page.locator('#rv2XrayRefresh').count(),0,'embedded Xray surface must not expose redundant manual status refresh');
    assert.equal(xrayActions.length,0,'opening Xray tab must remain read-only');

    await page.locator('#rv2XrayRestart').click();
    await page.waitForFunction(()=>document.querySelector('#rv2XrayNotice')?.textContent.includes('перезапущен'));
    assert.deepEqual(xrayActions,['restart'],'embedded Xray tab must use the canonical lifecycle mutation owner');

    await page.locator('#rv2XrayStop').click();
    await page.waitForFunction(()=>document.querySelector('#rv2XrayStatus')?.textContent==='Остановлен');
    assert.deepEqual(xrayActions,['restart','stop']);
    assert.equal(await page.locator('#rv2XrayStart').isVisible(),true);
    assert.equal(await page.locator('#rv2XrayVersions').isHidden(),true,'intentionally stopped Xray must not expose version mutation');

    await page.locator('#rv2XrayStart').click();
    await page.waitForFunction(()=>document.querySelector('#rv2XrayStatus')?.textContent==='Работает');
    assert.deepEqual(xrayActions,['restart','stop','start']);

    assert.equal((await page.locator('#rv2XrayVersions').textContent()).trim(),'Обновить','Routing Xray action must use update wording');
    await page.locator('#rv2XrayVersions').click();
    await page.waitForFunction(()=>document.querySelector('#xrayCoreManager') && !document.querySelector('#xrayCoreManager').hidden && (document.querySelector('#xrayCoreManager')?.innerText||'').includes('v26.10.1'));
    assert.equal(xrayCatalogGets,1,'Versions must reuse the existing lazy Xray Core Manager');
    await page.keyboard.press('Escape');
    await page.waitForFunction(()=>document.querySelector('#xrayCoreManager')?.hidden===true);

    await page.locator('.rv2-mode[data-mode="rules"]').click();
    await page.waitForSelector('#rv2RulesPanel',{state:'visible'});

    // Wide desktop canvas must use the available viewport instead of the old 1180px cap.
    const desktopLayout=await page.evaluate(()=> {
      const content=document.querySelector('.content')?.getBoundingClientRect();
      const boards=[...document.querySelectorAll('.rv4-board')].map(node=>node.getBoundingClientRect());
      return {
        contentWidth:content?.width||0,
        boardTops:boards.map(box=>Math.round(box.top)),
        scrollWidth:document.documentElement.scrollWidth,
        innerWidth:window.innerWidth
      };
    });
    assert.ok(desktopLayout.contentWidth>=1280,'desktop content canvas must be >=1280px at 1600px viewport, got '+desktopLayout.contentWidth);
    assert.equal(new Set(desktopLayout.boardTops).size,1,'DIRECT/VPN/BLOCK boards must remain in one desktop row');
    assert.ok(desktopLayout.scrollWidth<=desktopLayout.innerWidth+1,'wide canvas must not create horizontal overflow');

    // Routing UX v4 is an action board: destinations first, categories inside.
    await page.waitForFunction(()=>document.querySelector('#rv2LiveState')?.textContent.includes('4 пользовательских'));
    assert.equal(await page.locator('.rv4-board').count(),3,'DIRECT/VPN/BLOCK boards must always be visible');
    assert.equal(await page.locator('.rv4-board-add').count(),3,'each destination board must own its add action');
    assert.equal(await page.locator('.rv4-board-collapse').count(),0,'separate collapse arrows must be absent');
    assert.equal(await page.locator('.rv2-add-card').count(),0,'global add card must be gone');
    assert.equal(await page.locator('#rv2DirectCount').textContent(),'23');
    assert.equal(await page.locator('#rv2VPNCount').textContent(),'4');
    assert.equal(await page.locator('#rv2BlockCount').textContent(),'0');
    for (const id of ['#rv2DirectBoard','#rv2VPNBoard','#rv2BlockBoard']) {
      assert.equal(await page.locator(id).evaluate(el=>el.classList.contains('collapsed')),true,id+' must start collapsed');
      assert.equal(await page.locator(id+' .rv4-board-head').getAttribute('aria-expanded'),'false',id+' must expose collapsed aria state');
    }
    assert.equal(await page.locator('#rv2DirectContent').isHidden(),true,'DIRECT content must start hidden');
    assert.equal(await page.locator('#rv2VPNContent').isHidden(),true,'VPN content must start hidden');
    assert.equal(await page.locator('#rv2BlockContent').isHidden(),true,'BLOCK content must start hidden');

    const directText=await page.locator('#rv2DirectContent').innerText();
    const vpnText=await page.locator('#rv2VPNContent').innerText();
    const blockText=await page.locator('#rv2BlockContent').innerText();
    assert.match(directText,/GeoSite/);
    assert.match(directText,/Сайты/);
    assert.match(directText,/GeoIP/);
    assert.match(directText,/category-ru/);
    assert.match(directText,/4pda\.to/);
    assert.match(vpnText,/ru-blocked/);
    assert.match(blockText,/Ничего не блокируется/);
    assert.doesNotMatch(await page.locator('#rv2RulesPanel').innerText(),/05_routing\.json|inboundTag|dns-out|#4|#5|#6|#7/);
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,0,'live visualization must be read-only');

    // Categories are aggregated by type rather than repeated as Xray rule rows.
    // The board intentionally starts collapsed, so expand it before testing
    // visible chip density and the local "more" interaction.
    await page.locator('#rv2DirectBoard .rv4-board-head').click();
    assert.equal(await page.locator('#rv2DirectBoard').evaluate(el=>el.classList.contains('collapsed')),false,'manual expand must reveal DIRECT details');
    await page.locator('#rv2DirectBoard .rv4-board-head').click();
    assert.equal(await page.locator('#rv2DirectContent').isHidden(),true,'second header click must collapse DIRECT');
    await page.locator('#rv2DirectBoard .rv4-board-head').focus();
    await page.keyboard.press('Enter');
    assert.equal(await page.locator('#rv2DirectContent').isVisible(),true,'keyboard Enter must expand DIRECT');
    await page.locator('#rv2DirectBoard .rv4-board-add').click();
    assert.equal(await page.locator('#rv2DirectContent').isVisible(),true,'Add must not collapse the board');
    await page.locator('#rv2ComposerClose').click();

    assert.equal(await page.locator('#rv2DirectContent .rv4-type').count(),3,'DIRECT board should aggregate GeoSite/Sites/GeoIP');
    const geositeGroup=page.locator('#rv2DirectContent .rv4-type').first();
    assert.match(await geositeGroup.locator('.rv4-type-head').innerText(),/GeoSite/);
    assert.match(await geositeGroup.locator('.rv4-type-head').innerText(),/16/);
    assert.equal(await geositeGroup.locator('.rv4-chip:visible').count(),10);
    assert.equal(await geositeGroup.locator('.rv4-more').textContent(),'+6');
    const chipFont=parseFloat(await geositeGroup.locator('.rv4-chip').first().evaluate(el=>getComputedStyle(el).fontSize));
    assert.ok(chipFont>=13,'category chip font must stay readable, got '+chipFont);
    await geositeGroup.locator('.rv4-more').click();
    assert.equal(await geositeGroup.locator('.rv4-chip:visible').count(),16);
    assert.match(await geositeGroup.innerText(),/Свернуть/);
    assert.equal(await geositeGroup.locator('.rv4-chip').first().evaluate(el=>el.tagName),'BUTTON','live chips must be interactive controls');

    // Chip removal is only a draft until explicit validation/apply, and can be undone inline.
    const categoryRU=geositeGroup.locator('.rv4-chip').filter({hasText:'category-ru'}).first();
    const chipApplyBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    await categoryRU.click();
    assert.equal(await categoryRU.evaluate(el=>el.classList.contains('pending-remove')),true);
    assert.equal(await page.locator('#rv2DraftCard').isVisible(),true);
    assert.match(await page.locator('#rv2RuleList').innerText(),/Удалить · GeoSite · category-ru/);
    assert.equal(await page.locator('#rv2ApplyRules').isDisabled(),false,'deletion-only draft must be ready for one-click save');
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,chipApplyBefore,'staging removal must not mutate runtime');
    await categoryRU.click();
    assert.equal(await page.locator('#rv2DraftCard').isHidden(),true,'inline undo must clear the deletion-only draft');
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,chipApplyBefore,'undo must remain read-only');

    // Protected Xray service rules remain in the authoritative live config but
    // are not user-facing routing controls and must not be rendered in Rules.
    assert.equal(await page.locator('#rv2SystemWrap').count(),0);
    assert.equal(await page.locator('#rv2SystemToggle').count(),0);
    assert.equal(await page.locator('#rv2SystemList').count(),0);
    const rulesPanelText=await page.locator('#rv2RulesPanel').innerText();
    assert.doesNotMatch(rulesPanelText,/Служебные правила Xray|защищены FreeNet|Входящий трафик|DNS-запросы|Сетевой транспорт/);
    assert.equal(routingLive.routing.rules.length,8,'hiding service-rule details must not remove protected Xray rules');
    assert.equal(routingLive.routing.rules.filter(rule=>rule.inboundTag||rule.port==='53'||rule.network).length,4,'all protected service rules must remain authoritative');

    // Empty draft and inline composer stay out of the way until the user asks to add something.
    assert.equal(await page.locator('#rv2DraftCard').isHidden(),true);
    assert.equal(await page.locator('#rv2InlineComposer').isHidden(),true);

    // Common case: a new DIRECT domain is merged into the existing compatible DIRECT domain rule.
    const directApplyBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    const directValidateBefore=calls.filter(x=>x==='POST /api/routing/validate').length;
    const directDialogsBefore=dialogCount;
    await page.locator('.rv4-board-add[data-add-action="DIRECT"]').click();
    await page.locator('#rv2Kind').selectOption('domain');
    await page.locator('#rv2Value').fill('ifconfig.me');
    await page.locator('#rv2AddRule').click();
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);
    assert.match(await page.locator('#rv2RuleList').innerText(),/Сайт · ifconfig\.me/);
    assert.equal(routingLive.routing.rules.length,8,'draft must not mutate live routing');
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2RulesApplyResult')?.textContent.includes('Изменения применены'),null,{timeout:10000});
    assert.equal(calls.filter(x=>x==='POST /api/routing/validate').length,directValidateBefore+1,'one-click save must validate exactly once');
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,directApplyBefore+1,'one-click save must apply exactly once');
    assert.equal(dialogCount,directDialogsBefore,'Rules save must not open a browser confirm dialog');
    assert.equal(await page.locator('[data-page-view="routing"]').evaluate(el=>el.classList.contains('active')),true,'successful Rules apply must stay on Routing');
    assert.equal(routingLive.routing.rules.length,8,'compatible DIRECT domain selector must merge without adding an Xray rule object');
    assert.equal(routingLive.routing.rules[3].outboundTag,'direct');
    assert.equal(routingLive.routing.rules[3].domain.includes('domain:ifconfig.me'),true,'ifconfig.me must merge into existing DIRECT domain array');
    assert.equal(routingLive.routing.rules[0].domain?.includes?.('domain:ifconfig.me')||false,false,'merged selector must not be prepended as a separate rule');
    await page.waitForFunction(()=>document.querySelector('#rv2DirectCount')?.textContent==='24');
    assert.match(await page.locator('#rv2DirectContent').innerText(),/ifconfig\.me/);

    // The same selector can be removed with the same one-click path, still in-place.
    const ifconfigChip=page.locator('#rv2DirectContent .rv4-chip').filter({hasText:'ifconfig.me'}).first();
    await ifconfigChip.click();
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);
    const directRemoveBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2RulesApplyResult')?.textContent.includes('Изменения применены'),null,{timeout:10000});
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules')?.textContent==='Применить');
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,directRemoveBefore+1);
    assert.equal(routingLive.routing.rules.length,8,'selector removal must not drop the containing DIRECT rule');
    assert.equal(routingLive.routing.rules.some(rule=>Array.isArray(rule.domain)&&rule.domain.includes('domain:ifconfig.me')),false);
    await page.waitForFunction(()=>document.querySelector('#rv2DirectCount')?.textContent==='23');
    assert.equal(dialogCount,directDialogsBefore,'Rules deletion must not open a browser confirm dialog');

    // Boards start collapsed independently; Add expands only the selected board.
    assert.equal(await page.locator('#rv2VPNBoard').evaluate(el=>el.classList.contains('collapsed')),true);
    assert.equal(await page.locator('#rv2VPNContent').isHidden(),true);
    assert.equal(await page.locator('#rv2DirectContent').isVisible(),true,'DIRECT stays expanded after its Add/Edit workflow');

    // Add from the VPN board: action is contextual, no separate DIRECT/VPN/BLOCK switch is required.
    await page.locator('.rv4-board-add[data-add-action="VPN"]').click();
    assert.equal(await page.locator('#rv2VPNBoard').evaluate(el=>el.classList.contains('collapsed')),false,'Add must expand the selected board');
    assert.equal(await page.locator('#rv2InlineComposer').isVisible(),true);
    assert.equal(await page.locator('#rv2VPNComposerSlot #rv2InlineComposer').count(),1);
    assert.match(await page.locator('#rv2ComposerTitle').textContent(),/Добавить через VPN/);
    assert.match(await page.locator('#rv2ComposerHint').textContent(),/через текущий VPN/);

    // Smart GeoData autocomplete is the single Rules lookup surface: read-only, debounced, scrollable and keyboard-selectable.
    const geoMutationBefore = calls.filter(x => /^POST \/api\/(routing|action|network)/.test(x)).length;
    await page.locator('#rv2Kind').selectOption('geosite');

    // A transient host-shaped error while editing must not remain after a newer
    // successful category-prefix request.
    await page.locator('#rv2Value').fill('steam.');
    await page.waitForFunction(() => (document.querySelector('#rv2RuleNotice')?.textContent || '').includes('invalid host or URL'));
    await page.locator('#rv2Value').fill('you');
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/ищу локальные категории/);
    await page.waitForSelector('#rv2GeoAutocomplete .rv2-autocomplete-item');
    assert.doesNotMatch(await page.locator('#rv2RuleNotice').textContent(),/invalid host or URL/);
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/GeoData: найдено/);
    assert.ok(geoSuggestRequests.some(x => x.kind==='geosite' && x.q==='you' && x.mode==='prefix'),'bare category autocomplete must force prefix mode');

    // Losing focus while the read-only lookup is in flight must not abort it
    // and leave Rules stuck on the loading notice.
    const delayedRequest = page.waitForRequest(req => req.url().includes('/api/geodata/suggest') && req.url().includes('q=slow-blur'));
    await page.locator('#rv2Value').fill('slow-blur');
    await delayedRequest;
    await page.locator('#rv2AddRule').focus();
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/ищу локальные категории/);
    await page.waitForFunction(() => (document.querySelector('#rv2RuleNotice')?.textContent || '').includes('GeoData: найдено 1'));
    assert.equal(await page.locator('#rv2GeoAutocomplete').isHidden(),true,'blur may hide popup but must not abort lookup completion');

    await page.locator('#rv2Value').focus();
    await page.waitForSelector('#rv2GeoAutocomplete .rv2-autocomplete-item');
    assert.match(await page.locator('#rv2GeoAutocomplete').innerText(),/slow-blur-result/);

    // Reproduce the real-router layout where the next routing card sits below
    // the open composer. The popup must leave the board stacking context and
    // paint as a viewport overlay above that following card.
    await page.setViewportSize({width:700,height:900});
    await page.locator('#rv2Value').evaluate(el => el.scrollIntoView({block:'start'}));
    await page.locator('#rv2Value').fill('many');
    await page.waitForFunction(() => document.querySelectorAll('#rv2GeoAutocomplete .rv2-autocomplete-item').length === 24);
    const geoMenu = await page.locator('#rv2GeoAutocomplete').evaluate(el => {
      const style = getComputedStyle(el);
      const before = el.scrollTop;
      el.scrollTop = el.scrollHeight;
      const box = el.getBoundingClientRect();
      const block = document.querySelector('#rv2BlockBoard')?.getBoundingClientRect();
      const vpn = document.querySelector('#rv2VPNBoard');
      return {
        clientHeight: el.clientHeight,
        scrollHeight: el.scrollHeight,
        scrolled: el.scrollTop > before,
        overflowY: style.overflowY,
        position: style.position,
        zIndex: Number(style.zIndex || 0),
        parentIsBody: el.parentElement === document.body,
        insideVPNBoard: !!vpn?.contains(el),
        top: box.top,
        bottom: box.bottom,
        left: box.left,
        right: box.right,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight,
        overlapsFollowingBlock: !!block && box.bottom > block.top && box.top < block.bottom && box.right > block.left && box.left < block.right
      };
    });
    assert.equal(geoMenu.parentIsBody,true,'GeoData popup must be portaled to document.body');
    assert.equal(geoMenu.insideVPNBoard,false,'GeoData popup must not remain inside the routing board stacking context');
    assert.equal(geoMenu.position,'fixed','GeoData popup must use viewport-fixed geometry');
    assert.ok(geoMenu.zIndex >= 10000,'GeoData popup must paint above routing cards');
    assert.ok(geoMenu.top >= 0 && geoMenu.bottom <= geoMenu.viewportHeight + 1,'GeoData popup must stay inside the visible viewport');
    assert.ok(geoMenu.left >= 0 && geoMenu.right <= geoMenu.viewportWidth + 1,'GeoData popup must stay inside viewport width');
    assert.ok(geoMenu.scrollHeight > geoMenu.clientHeight,'GeoData menu must expose all lower results through scrolling');
    assert.equal(geoMenu.overflowY,'auto');
    assert.equal(geoMenu.scrolled,true,'GeoData menu must actually scroll to lower results');
    assert.equal(geoMenu.overlapsFollowingBlock,true,'real stacked layout must prove popup overlays the following BLOCK card instead of being clipped by it');
    assert.equal(await page.locator('#rv2GeoSearch').count(),0,'redundant manual GeoData button must be removed');
    await page.setViewportSize({width:1600,height:1000});

    await page.locator('#rv2Value').fill('you');
    await page.waitForSelector('#rv2GeoAutocomplete .rv2-autocomplete-item');
    assert.equal(await page.locator('#rv2GeoAutocomplete .rv2-autocomplete-item').count(),1);
    assert.doesNotMatch(await page.locator('#rv2GeoAutocomplete').innerText(),/youtube-extra/);
    await page.locator('#rv2Value').press('ArrowDown');
    await page.locator('#rv2Value').press('ArrowUp');
    await page.locator('#rv2Value').press('Enter');
    assert.equal(await page.locator('#rv2Value').inputValue(),'youtube');
    assert.equal(await page.locator('#rv2GeoAutocomplete').isHidden(),true);
    assert.match(await page.locator('#rv2RuleNotice').textContent(),/Нажмите «Добавить»/);

    const geoMutationCalls = calls.filter(x => /^POST \/api\/(routing|action|network)/.test(x)).length;
    assert.equal(geoMutationCalls,geoMutationBefore,'GeoData autocomplete must remain fully read-only');

    // GeoIP accepts a host, resolves it through the bounded backend and exposes A/AAAA evidence without creating a draft.
    await page.locator('#rv2Kind').selectOption('geoip');
    await page.locator('#rv2Value').fill('example.com');
    await page.waitForSelector('#rv2GeoAutocomplete .rv2-autocomplete-item');
    assert.match(await page.locator('#rv2GeoAutocomplete').innerText(),/1\.1\.1\.7/);
    assert.match(await page.locator('#rv2GeoAutocomplete').innerText(),/8\.8\.8\.8/);
    assert.ok(geoSuggestRequests.some(x => x.kind==='geoip' && x.q==='example.com' && x.mode===''),'host-shaped GeoIP autocomplete must keep automatic DNS mode');
    await page.locator('#rv2Value').press('Escape');
    assert.equal(await page.locator('#rv2GeoAutocomplete').isHidden(),true);
    assert.equal(await page.locator('#rv2RuleList .rv2-rule').count(),0,'autocomplete must not create a draft');

    // Return to GeoSite and choose the standard category for the existing apply scenario.
    await page.locator('#rv2Kind').selectOption('geosite');
    await page.locator('#rv2Value').fill('you');
    await page.waitForSelector('#rv2GeoAutocomplete .rv2-autocomplete-item');
    await page.locator('#rv2Value').press('Tab');
    assert.equal(await page.locator('#rv2Value').inputValue(),'youtube');

    // Empty draft has only one save action and it is disabled until a change exists.
    assert.equal(await page.locator('#rv2ValidateRules').count(),0);
    assert.equal(await page.locator('#rv2ApplyRules').isDisabled(),true);
    assert.equal(await page.locator('#rv2ApplyRules').textContent(),'Применить');

    // Add the GeoSite to VPN without touching the live board; one click validates and applies.
    await page.locator('#rv2AddRule').click();
    await page.waitForFunction(()=>document.querySelectorAll('#rv2RuleList .rv2-rule').length===1);
    assert.match(await page.locator('#rv2RuleList').innerText(),/GeoSite · youtube/);
    assert.match(await page.locator('#rv2RuleList').innerText(),/VPN/);
    assert.equal(await page.locator('#rv2DirectCount').textContent(),'23','draft must not rewrite live DIRECT board before apply');
    assert.equal(await page.locator('#rv2VPNCount').textContent(),'4','draft must not rewrite live VPN board before apply');
    assert.equal(await page.locator('#rv2DraftCard').isVisible(),true,'draft card appears only after the first change');
    assert.equal(await page.locator('#rv2DraftCount').textContent(),'1');
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);

    const visualBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    const visualValidateBefore=calls.filter(x=>x==='POST /api/routing/validate').length;
    const visualDialogsBefore=dialogCount;
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2RulesApplyResult')?.textContent.includes('Изменения применены'),null,{timeout:10000});
    const visualAfter=calls.filter(x=>x==='POST /api/routing/apply').length;
    assert.equal(visualAfter,visualBefore+1,'visual rules save must issue exactly one transactional mutation');
    assert.equal(calls.filter(x=>x==='POST /api/routing/validate').length,visualValidateBefore+1,'visual rules save must validate exactly once');
    assert.equal(dialogCount,visualDialogsBefore,'Rules save must not show native confirm');
    assert.equal(routingLive.routing.rules.length,9,'precedence conflict must keep the new VPN rule prepended instead of unsafe merge');
    assert.deepEqual(routingLive.routing.rules[0],{type:'field',outboundTag:'vless-reality',domain:['geosite:youtube']});
    assert.deepEqual(routingLive.routing.rules[8],{type:'field',network:'tcp,udp',outboundTag:'vless-reality'},'complex existing rule must be preserved exact');

    // SUCCESS rereads live state in-place and keeps the current Routing page.
    assert.equal(await page.locator('[data-page-view="routing"]').evaluate(el=>el.classList.contains('active')),true);
    await page.waitForFunction(()=>document.querySelector('#rv2LiveState')?.textContent.includes('5 пользовательских'));
    assert.equal(await page.locator('#rv2DirectCount').textContent(),'23');
    assert.equal(await page.locator('#rv2VPNCount').textContent(),'5');
    assert.match(await page.locator('#rv2VPNContent').innerText(),/youtube/);

    // Deletion uses the same one-click validate/apply path and stays in-place.
    const removeApplyBefore=calls.filter(x=>x==='POST /api/routing/apply').length;
    const liveCategoryRU=page.locator('#rv2DirectContent .rv4-chip').filter({hasText:'category-ru'}).first();
    await liveCategoryRU.click();
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,removeApplyBefore,'live chip click must only stage a deletion');
    assert.match(await page.locator('#rv2RuleList').innerText(),/Удалить · GeoSite · category-ru/);
    await page.waitForFunction(()=>document.querySelector('#rv2ApplyRules') && !document.querySelector('#rv2ApplyRules').disabled);
    await page.locator('#rv2ApplyRules').click();
    await page.waitForFunction(()=>document.querySelector('#rv2RulesApplyResult')?.textContent.includes('Изменения применены'),null,{timeout:10000});
    assert.equal(calls.filter(x=>x==='POST /api/routing/apply').length,removeApplyBefore+1,'deletion must use exactly one controlled apply');
    assert.equal(routingLive.routing.rules.length,9,'removing one selector must not drop its containing rule or unrelated rules');
    assert.equal(routingLive.routing.rules.some(rule=>Array.isArray(rule.domain)&&rule.domain.includes('ext:geosite.dat:category-ru')),false,'selected live selector must be removed');
    assert.equal(routingLive.routing.rules.some(rule=>Array.isArray(rule.domain)&&rule.domain.includes('ext:geosite.dat:google')),true,'unrelated selectors must remain');
    assert.deepEqual(routingLive.routing.rules[8],{type:'field',network:'tcp,udp',outboundTag:'vless-reality'},'system/complex rule must remain exact after chip deletion');
    assert.equal(await page.locator('[data-page-view="routing"]').evaluate(el=>el.classList.contains('active')),true);
    assert.doesNotMatch(await page.locator('#rv2DirectContent').innerText(),/category-ru/);
    assert.match(await page.locator('#rv2DirectContent').innerText(),/google/);

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
    assert.match(result,/Результат: APPLIED/);assert.match(result,/Откат: NOT_NEEDED/);assert.match(result,/Xray Core restarted/);
    assert.equal(errors.length,0,errors.join('\n'));assert.equal(consoleErrors.length,0,consoleErrors.join('\n'));
    console.log('ROUTING_V2_RUNTIME',JSON.stringify(pre));console.log('ROUTING_V2_APPLY_CALLS',after);
  }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;});
