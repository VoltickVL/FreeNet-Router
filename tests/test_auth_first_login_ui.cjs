// Browser regression for first-run authentication on slow Keenetic/MIPS.
// Routes are simulated: no router, real credentials or external endpoints used.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');

const web = path.resolve(__dirname, '..', 'freenet-ui', 'web');
const server = http.createServer((req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/') {
    const html = fs.readFileSync(path.join(web, 'index.html'), 'utf8')
      .replace('</body>', '<script src="/accepted-ux.js"></script></body>');
    res.writeHead(200, {'Content-Type':'text/html; charset=utf-8'});
    res.end(html);
    return;
  }
  if (pathname === '/accepted-ux.js') {
    res.writeHead(200, {'Content-Type':'application/javascript; charset=utf-8'});
    res.end(fs.readFileSync(path.join(web, 'accepted-ux.js'), 'utf8'));
    return;
  }
  res.writeHead(404);
  res.end();
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const browser = await chromium.launch({headless:true});
  const base = 'http://127.0.0.1:' + server.address().port;
  const password = 'fixture-password-123456';
  try {
    async function scenario({configured, response, delayed=false}) {
      const page = await browser.newPage({viewport:{width:1280,height:800}});
      let authStatusCalls = 0;
      let authPostCalls = 0;
      let body = null;
      let releasePost;
      const held = new Promise(resolve => {releasePost=resolve;});
      const answer = (route, payload, status=200) => route.fulfill({
        status, contentType:'application/json', body:JSON.stringify(payload)
      });
      await page.route('**/api/**', async route => {
        const request = route.request(), routePath = new URL(request.url()).pathname;
        if (routePath === '/api/auth/status') {
          authStatusCalls++;
          return answer(route, {configured,authenticated:false});
        }
        if (request.method() === 'POST' && (routePath === '/api/auth/setup' || routePath === '/api/auth/login')) {
          authPostCalls++;
          body = request.postDataJSON();
          if (delayed) await held;
          if (response === 'network') return route.abort('failed');
          if (response === 'unauthorized') return answer(route, {error:'invalid credentials'}, 401);
          return answer(route, {configured:true,authenticated:true}, configured ? 200 : 201);
        }
        if (routePath === '/api/status') {
          return answer(route, {
            version:'0.6.21',country:'',city:'',country_code:'',endpoint:'',
            dns_mode:'direct',dns_out_present:true,xray_online:true,
            xkeen_ui_online:false,setup_complete:false,install_scenario:'existing_stack',
            subscription_configured:false,busy:false,updater_busy:false
          });
        }
        if (routePath === '/api/network-profile/plan') {
          return answer(route, {success:true,supported:true,active:false,extra_profiles:[]});
        }
        return answer(route, {success:true,available:false,configured:false,events:[],settings:{}});
      });
      await page.goto(base + '/#overview');
      await page.waitForSelector('#authRemember', {state:'attached'});
      return {
        page,
        get authStatusCalls(){return authStatusCalls;},
        get authPostCalls(){return authPostCalls;},
        get body(){return body;},
        releasePost
      };
    }

    // Setup: immediate busy UI, Enter and double click must not re-submit.
    const setup = await scenario({configured:false,response:'success',delayed:true});
    await setup.page.locator('#authPassword').fill(password);
    await setup.page.locator('#authPasswordConfirm').fill(password);
    await setup.page.locator('#authRemember').check();
    const setupChecks = setup.authStatusCalls;
    const setupRequest = setup.page.waitForRequest(req=>req.url().endsWith('/api/auth/setup') && req.method()==='POST');
    await setup.page.locator('#authSubmitBtn').click();
    await setupRequest;
    await setup.page.waitForFunction(()=>document.querySelector('#authSubmitBtn').getAttribute('aria-busy') === 'true');
    assert.equal(await setup.page.locator('#authSubmitBtn').isDisabled(), true);
    assert.match(await setup.page.locator('#authNotice').innerText(), /не обновляйте страницу/);
    assert.equal(await setup.page.locator('#controlCenter').isVisible(), false);
    await setup.page.evaluate(()=>{
      document.querySelector('#authSubmitBtn').dispatchEvent(new MouseEvent('click',{bubbles:true}));
      document.querySelector('#authPassword').dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));
    });
    assert.equal(setup.authPostCalls,1);
    assert.equal(setup.body.remember,true,'remember session must survive pending state');
    setup.releasePost();
    await setup.page.waitForFunction(()=>!document.querySelector('#controlCenter').hidden);
    assert.equal(await setup.page.locator('#authSection').isVisible(),false);
    assert.equal(setup.authStatusCalls,setupChecks,'confirmed POST must not force a second auth status request');
    assert.equal(await setup.page.locator('#authPassword').inputValue(),'');
    await setup.page.close();

    // Wrong password: controls re-enabled and error visible, protected UI hidden.
    const rejected = await scenario({configured:true,response:'unauthorized',delayed:true});
    await rejected.page.locator('#authPassword').fill(password);
    const rejectedRequest = rejected.page.waitForRequest(req=>req.url().endsWith('/api/auth/login') && req.method()==='POST');
    await rejected.page.locator('#authSubmitBtn').click();
    await rejectedRequest;
    assert.match(await rejected.page.locator('#authNotice').innerText(), /Проверяем пароль/);
    rejected.releasePost();
    await rejected.page.waitForFunction(()=>!document.querySelector('#authSubmitBtn').disabled);
    assert.match(await rejected.page.locator('#authNotice').innerText(), /invalid credentials/);
    assert.equal(await rejected.page.locator('#controlCenter').isVisible(),false);
    assert.equal(rejected.authPostCalls,1);
    await rejected.page.close();

    // Uncertain network response: no automatic mutation retry or false success.
    const uncertain = await scenario({configured:false,response:'network',delayed:true});
    await uncertain.page.locator('#authPassword').fill(password);
    await uncertain.page.locator('#authPasswordConfirm').fill(password);
    const uncertainRequest = uncertain.page.waitForRequest(req=>req.url().endsWith('/api/auth/setup') && req.method()==='POST');
    await uncertain.page.locator('#authSubmitBtn').click();
    await uncertainRequest;
    uncertain.releasePost();
    await uncertain.page.waitForFunction(()=>!document.querySelector('#authSubmitBtn').disabled);
    assert.match(await uncertain.page.locator('#authNotice').innerText(), /Пароль мог сохраниться/);
    assert.equal(await uncertain.page.locator('#controlCenter').isVisible(),false);
    assert.equal(uncertain.authPostCalls,1);
    await uncertain.page.close();

    // Normal login: one confirmed HTTP 200, no second GET /auth/status.
    const login = await scenario({configured:true,response:'success'});
    await login.page.locator('#authPassword').fill(password);
    const loginChecks = login.authStatusCalls;
    await login.page.locator('#authSubmitBtn').click();
    await login.page.waitForFunction(()=>!document.querySelector('#controlCenter').hidden);
    assert.equal(login.authPostCalls,1);
    assert.equal(login.authStatusCalls,loginChecks);
    await login.page.close();
    console.log('Auth first-login slow-MIPS browser regression PASS');
  } finally {
    await browser.close();
    await new Promise(resolve=>server.close(resolve));
  }
})().catch(err=>{console.error(err);process.exitCode=1;server.close();});
