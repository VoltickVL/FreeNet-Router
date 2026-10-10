package main

const controlCenterLayoutCoherenceStyle = `<style id="freenetLayoutCoherenceStyles">
html{scrollbar-gutter:stable}
#controlCenter .main{--fn-center-max:1180px;--fn-center-gutter:24px}
body #controlCenter .main .content{width:min(var(--fn-center-max),calc(100% - 2 * var(--fn-center-gutter)))!important;margin-left:auto!important;margin-right:auto!important}
body #controlCenter .main>.topbar.overview-approved{padding-right:max(var(--fn-center-gutter),calc((100% - var(--fn-center-max)) / 2))!important}
#controlCenter .fn3-dns-layout{grid-template-columns:minmax(0,1fr)!important}
#controlCenter .fn3-dns-modes{grid-template-columns:repeat(2,minmax(0,1fr))!important}
#controlCenter .fn3-dns-resolvers{grid-template-columns:repeat(2,minmax(0,1fr))!important}
#controlCenter .page.active{animation:freenetPageEnter .16s cubic-bezier(.2,.7,.2,1) both}
@keyframes freenetPageEnter{from{opacity:.92;transform:translateY(4px)}to{opacity:1;transform:translateY(0)}}
@media(max-width:820px){#controlCenter .main{--fn-center-gutter:10px}body #controlCenter .main .content{width:calc(100% - 20px)!important}}
@media(max-width:720px){#controlCenter .fn3-dns-modes,#controlCenter .fn3-dns-resolvers{grid-template-columns:minmax(0,1fr)!important}}
@media(prefers-reduced-motion:reduce){#controlCenter .page.active{animation:none!important}}
</style>`
