const state = {
  route: window.location.hash || '#/',
  token: sessionStorage.getItem('agentpaas.dashboard.token') || '',
  resources: {
    agents: [],
    gateways: [],
    mcp_servers: []
  }
};

const app = document.querySelector('#app');

function setRoute() {
  state.route = window.location.hash || '#/';
  render();
}

function authHeader() {
  if (!state.token) {
    return {};
  }
  return { Authorization: `Bearer ${state.token}` };
}

async function loadResources() {
  if (!state.token) {
    render();
    return;
  }
  const response = await fetch('/api/resources', {
    headers: authHeader()
  });
  if (response.ok) {
    state.resources = await response.json();
  }
  render();
}

function renderList(items, emptyText, itemTemplate) {
  if (!items || items.length === 0) {
    return `<p class="empty">${emptyText}</p>`;
  }
  return `<ul>${items.map(itemTemplate).join('')}</ul>`;
}

function escapeText(value) {
  return String(value || '').replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }[char]));
}

function renderPanel(title, content) {
  return `<section class="panel" tabindex="-1"><h2>${title}</h2>${content}</section>`;
}

function routeContent() {
  const { agents, gateways, mcp_servers: mcpServers } = state.resources;
  if (state.route === '#/agents') {
    return renderPanel('Agents', renderList(agents, 'No agents are managed yet.', agentItem));
  }
  if (state.route === '#/gateways') {
    return renderPanel('Gateways', renderList(gateways, 'No gateways are managed yet.', gatewayItem));
  }
  if (state.route === '#/mcp-servers') {
    return renderPanel('MCP Servers', renderList(mcpServers, 'No MCP servers are managed yet.', mcpItem));
  }
  return [
    renderPanel('Agents', renderList(agents, 'No agents are managed yet.', agentItem)),
    renderPanel('Gateways', renderList(gateways, 'No gateways are managed yet.', gatewayItem)),
    renderPanel('MCP Servers', renderList(mcpServers, 'No MCP servers are managed yet.', mcpItem))
  ].join('');
}

function agentItem(agent) {
  return `<li><strong>${escapeText(agent.name || agent.id)}</strong><span>${escapeText(agent.status)}</span><span>${escapeText(agent.health)}</span></li>`;
}

function gatewayItem(gateway) {
  return `<li><strong>${escapeText(gateway.id)}</strong><span>${escapeText(gateway.status)}</span><span>${escapeText(gateway.health)}</span></li>`;
}

function mcpItem(server) {
  return `<li><strong>${escapeText(server.id)}</strong><span>${escapeText(server.status)}</span><span>${escapeText(server.type)}</span></li>`;
}

function login(event) {
  event.preventDefault();
  const data = new FormData(event.currentTarget);
  state.token = String(data.get('apiKey') || '');
  sessionStorage.setItem('agentpaas.dashboard.token', state.token);
  loadResources();
}

function render() {
  app.innerHTML = `
    <header>
      <h1>AgentPaaS Dashboard</h1>
      <nav aria-label="Dashboard sections">
        <a href="#/">Overview</a>
        <a href="#/agents">Agents</a>
        <a href="#/gateways">Gateways</a>
        <a href="#/mcp-servers">MCP Servers</a>
      </nav>
    </header>
    <main>
      <form class="auth" aria-label="API key" data-auth-form>
        <label for="api-key">API key</label>
        <input id="api-key" name="apiKey" type="password" autocomplete="off">
        <button type="submit">Connect</button>
      </form>
      ${routeContent()}
    </main>
  `;
  const form = app.querySelector('[data-auth-form]');
  form.addEventListener('submit', login);
}

window.addEventListener('hashchange', setRoute);
render();
loadResources();
