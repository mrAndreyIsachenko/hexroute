package dashboard

const loginHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Hexroute</title>
  <link rel="stylesheet" href="/assets/app.css">
  <script src="/assets/app.js" defer></script>
</head>
<body class="auth-page" data-page="login">
  <main class="auth-panel">
    <div class="brand">Hexroute</div>
    <h1>Operations access</h1>
    <label>Username<input id="username" autocomplete="username webauthn"></label>
    <button id="login" type="button">Sign in with passkey</button>
    <details>
      <summary>First passkey</summary>
      <label>Bootstrap token<input id="bootstrap" type="password" autocomplete="off"></label>
      <button id="register-first" class="secondary" type="button">Register passkey</button>
    </details>
    <p id="status" role="status"></p>
  </main>
</body>
</html>`

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Hexroute status</title>
  <link rel="stylesheet" href="/assets/app.css">
  <script src="/assets/app.js" defer></script>
</head>
<body data-page="dashboard" data-username="{{.Username}}">
  <header>
    <div><span class="brand">Hexroute</span><span class="muted">operations</span></div>
    <nav>
      <button id="register-more" class="secondary" type="button">Add passkey</button>
      <button id="logout" class="secondary" type="button">Sign out</button>
    </nav>
  </header>
  <main>
    <section>
      <div class="section-title"><h1>Nodes</h1><time>{{time .Snapshot.GeneratedAt}}</time></div>
      <div class="table-wrap"><table>
        <thead><tr><th>Name</th><th>Kind</th><th>Freshness</th><th>Components</th></tr></thead>
        <tbody>{{range .Snapshot.Nodes}}<tr>
          <td>{{.Name}}</td><td>{{.Kind}}</td>
          <td><span class="state {{if .Stale}}bad{{else}}ok{{end}}">{{if .Stale}}stale{{else}}current{{end}}</span></td>
          <td>{{range .Components}}<span class="state {{class .Health}}">{{.Name}}: {{.Health}}</span> {{end}}</td>
        </tr>{{else}}<tr><td colspan="4" class="empty">No nodes</td></tr>{{end}}</tbody>
      </table></div>
    </section>
    <section>
      <div class="section-title"><h2>Connectivity read model</h2><span class="muted">observe-only</span></div>
      <div class="table-wrap"><table>
        <thead><tr><th>Node</th><th>Aggregate</th><th>Authorization</th><th>Integrity</th><th>Generations</th><th>Components</th><th>Proposals</th><th>Reported</th></tr></thead>
        <tbody>{{range .Snapshot.Connectivities}}<tr>
          <td>{{.Name}}</td>
          <td><span class="state {{class .Aggregate}}">{{.Aggregate}}</span></td>
          <td>{{.Authorization}}{{if ne .AuthorizationReason "none"}} <span class="muted">({{.AuthorizationReason}})</span>{{end}}</td>
          <td>{{.OpenGaps}} gaps{{if .GapOverflow}} <span class="state bad">dropped</span>{{end}},
              {{.SourceConflicts}} conflicts{{if .ConflictOverflow}} <span class="state bad">evicted</span>{{end}},
              {{.AwaitingBaseline}} awaiting baseline{{if .LineageReset}} <span class="state bad">lineage reset</span>{{end}}</td>
          <td>snapshot {{.SnapshotGeneration}} / bundle {{.BundleGeneration}} / root {{.RootGeneration}} / user {{.UserGeneration}}</td>
          <td>{{range .Components}}<span class="state {{class .State}}">{{.Name}}: {{.State}}/{{.Freshness}}</span> {{end}}</td>
          <td>{{range .ProposalClasses}}<span class="state">{{.Class}}: {{.Count}}</span> {{else}}<span class="muted">none</span>{{end}}</td>
          <td><span class="state {{if .Stale}}bad{{else}}ok{{end}}">{{time .ObservedAt}}</span></td>
        </tr>{{else}}<tr><td colspan="8" class="empty">No connectivity projections</td></tr>{{end}}</tbody>
      </table></div>
    </section>
    <section>
      <div class="section-title"><h2>Incidents</h2></div>
      <div class="table-wrap"><table>
        <thead><tr><th>Status</th><th>Severity</th><th>Component</th><th>Category</th><th>Generation</th><th>Observed</th></tr></thead>
        <tbody>{{range .Snapshot.Incidents}}<tr>
          <td><span class="state {{class .Status}}">{{.Status}}</span></td>
          <td>{{.Severity}}</td><td>{{.Component}}</td><td>{{.Category}}</td><td>{{.Generation}}</td><td>{{time .LastObservedAt}}</td>
        </tr>{{else}}<tr><td colspan="6" class="empty">No incidents</td></tr>{{end}}</tbody>
      </table></div>
    </section>
    <section class="split">
      <div><div class="section-title"><h2>Workers</h2></div>
        <div class="table-wrap"><table><thead><tr><th>Name</th><th>Version</th><th>Heartbeat</th></tr></thead>
        <tbody>{{range .Snapshot.Workers}}<tr><td>{{.Name}}</td><td>{{.ApplicationVersion}}</td>
        <td><span class="state {{if .Stale}}bad{{else}}ok{{end}}">{{time .HeartbeatAt}}</span></td></tr>
        {{else}}<tr><td colspan="3" class="empty">No workers</td></tr>{{end}}</tbody></table></div>
      </div>
      <div><div class="section-title"><h2>Deployments</h2></div>
        <div class="table-wrap"><table><thead><tr><th>Target</th><th>Release</th><th>Status</th><th>Config</th></tr></thead>
        <tbody>{{range .Snapshot.Deployments}}<tr><td>{{.TargetKey}}</td><td>{{.ApplicationVersion}}</td>
        <td><span class="state {{class .Status}}">{{.Status}}</span></td><td>{{.ConfigVersion}}</td></tr>
        {{else}}<tr><td colspan="4" class="empty">No deployments</td></tr>{{end}}</tbody></table></div>
      </div>
    </section>
    <section>
      <div class="section-title"><h2>SLO windows</h2></div>
      <div class="table-wrap"><table>
        <thead><tr><th>Service</th><th>Target</th><th>Objective</th><th>Result</th><th>Window</th></tr></thead>
        <tbody>{{range .Snapshot.SLOs}}<tr><td>{{.Service}}</td><td>{{.TargetKey}}</td><td>{{.Objective}}</td>
        <td>{{ratio .}}</td><td>{{time .WindowStart}}</td></tr>
        {{else}}<tr><td colspan="5" class="empty">No SLO windows</td></tr>{{end}}</tbody>
      </table></div>
    </section>
    <p id="status" role="status"></p>
  </main>
</body>
</html>`

const unavailableHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Hexroute unavailable</title><link rel="stylesheet" href="/assets/app.css"></head><body class="auth-page"><main class="auth-panel"><div class="brand">Hexroute</div><h1>Status unavailable</h1></main></body></html>`

const appCSS = `:root{color-scheme:dark;--bg:#111315;--panel:#191c1f;--line:#30353a;--text:#f2f4f5;--muted:#9ba4ac;--green:#64d391;--amber:#e7b65e;--red:#ed7373;--accent:#57a7e8}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 ui-sans-serif,system-ui,-apple-system,sans-serif;letter-spacing:0}header{height:58px;display:flex;align-items:center;justify-content:space-between;padding:0 24px;border-bottom:1px solid var(--line);background:#15181a;position:sticky;top:0}main{max-width:1500px;margin:0 auto;padding:22px 24px 48px}.brand{font-weight:750;font-size:18px;margin-right:10px}.muted,time{color:var(--muted)}nav{display:flex;gap:8px}section{margin:0 0 30px}.section-title{display:flex;align-items:baseline;justify-content:space-between;margin-bottom:10px}h1,h2{font-size:16px;margin:0;letter-spacing:0}table{width:100%;border-collapse:collapse;min-width:680px}th,td{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top}th{font-size:12px;color:var(--muted);font-weight:650;background:#171a1d}.table-wrap{border:1px solid var(--line);border-radius:6px;overflow:auto;background:var(--panel)}.state{display:inline-block;font-size:12px;white-space:nowrap}.state:before{content:"";display:inline-block;width:7px;height:7px;border-radius:50%;background:var(--muted);margin-right:6px}.state.ok:before{background:var(--green)}.state.warn:before{background:var(--amber)}.state.bad:before{background:var(--red)}.split{display:grid;grid-template-columns:1fr 1fr;gap:20px}.empty{color:var(--muted);text-align:center;padding:24px}.auth-page{min-height:100vh;display:grid;place-items:center}.auth-panel{width:min(380px,calc(100vw - 32px));border:1px solid var(--line);background:var(--panel);padding:26px;border-radius:6px}.auth-panel h1{margin:12px 0 22px;font-size:22px}label{display:block;color:var(--muted);font-size:12px;margin:12px 0}input{display:block;width:100%;margin-top:6px;background:#101214;border:1px solid var(--line);border-radius:4px;color:var(--text);padding:10px 11px;font:inherit}button{border:0;border-radius:4px;background:var(--accent);color:#071018;padding:9px 12px;font-weight:700;cursor:pointer}button.secondary{background:#2a3035;color:var(--text);border:1px solid var(--line)}.auth-panel>button{width:100%;margin-top:8px}details{margin-top:18px;border-top:1px solid var(--line);padding-top:14px}summary{color:var(--muted);cursor:pointer}#status{min-height:20px;color:var(--amber)}@media(max-width:900px){.split{grid-template-columns:1fr}header{padding:0 14px}main{padding:18px 14px}nav .secondary{max-width:130px;white-space:normal}}`

const appJavaScript = `"use strict";
const b64d=s=>Uint8Array.from(atob(s.replace(/-/g,"+").replace(/_/g,"/").padEnd(Math.ceil(s.length/4)*4,"=")),c=>c.charCodeAt(0));
const b64e=b=>{const a=new Uint8Array(b);let s="";for(let i=0;i<a.length;i+=32768)s+=String.fromCharCode(...a.subarray(i,i+32768));return btoa(s).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/,"")};
function creation(o){o.publicKey.challenge=b64d(o.publicKey.challenge);o.publicKey.user.id=b64d(o.publicKey.user.id);(o.publicKey.excludeCredentials||[]).forEach(x=>x.id=b64d(x.id));return o.publicKey}
function assertion(o){o.publicKey.challenge=b64d(o.publicKey.challenge);(o.publicKey.allowCredentials||[]).forEach(x=>x.id=b64d(x.id));return o.publicKey}
function credential(c){const r=c.response,v={id:c.id,rawId:b64e(c.rawId),type:c.type,authenticatorAttachment:c.authenticatorAttachment,clientExtensionResults:c.getClientExtensionResults(),response:{clientDataJSON:b64e(r.clientDataJSON)}};if(r.attestationObject)v.response.attestationObject=b64e(r.attestationObject);if(r.authenticatorData)v.response.authenticatorData=b64e(r.authenticatorData);if(r.signature)v.response.signature=b64e(r.signature);if(r.userHandle)v.response.userHandle=b64e(r.userHandle);if(r.getTransports)v.response.transports=r.getTransports();return v}
async function post(url,body,extra={}){const r=await fetch(url,{method:"POST",credentials:"same-origin",headers:{"Content-Type":"application/json",...extra},body:JSON.stringify(body)});if(!r.ok)throw new Error("request_failed");return r.status===204?{}:r.json()}
function status(message){const node=document.getElementById("status");if(node)node.textContent=message}
async function login(username){const options=await post("/auth/login/begin",{username});const c=await navigator.credentials.get({publicKey:assertion(options)});await post("/auth/login/finish",credential(c));location.href="/"}
async function register(username,bootstrap){const headers=bootstrap?{"X-Hexroute-Bootstrap":bootstrap}:{};const options=await post("/auth/register/begin",{username},headers);const c=await navigator.credentials.create({publicKey:creation(options)});await post("/auth/register/finish",credential(c));location.href="/"}
document.addEventListener("DOMContentLoaded",()=>{const page=document.body.dataset.page;if(page==="login"){document.getElementById("login").onclick=async()=>{try{status("");await login(document.getElementById("username").value)}catch(e){status("Authentication failed")}};document.getElementById("register-first").onclick=async()=>{try{status("");await register(document.getElementById("username").value,document.getElementById("bootstrap").value)}catch(e){status("Registration failed")}}}if(page==="dashboard"){document.getElementById("logout").onclick=async()=>{try{await post("/auth/logout",{});location.href="/login"}catch(e){status("Sign out failed")}};document.getElementById("register-more").onclick=async()=>{try{status("");await register(document.body.dataset.username,"")}catch(e){status("Registration failed")}}}});`
