const state = {
  route: window.location.hash || '#/',
  token: sessionStorage.getItem('agentpaas.dashboard.token') || '',
  resources: {
    agents: [],
    gateways: [],
    mcp_servers: []
  },
  timeline: {
    runID: '',
    rows: [],
    lastEventID: '',
    controller: null,
    reconnectTimer: 0,
    status: 'idle'
  },
  logViewer: {
    runID: '',
    rows: [],
    loading: false,
    error: ''
  }
};

const app = document.querySelector('#app');
const TIMELINE_ROW_HEIGHT = 58;
const TIMELINE_BUFFER = 8;

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
  const logViewerRunID = logViewerRunIDFromRoute();
  if (logViewerRunID) {
    return renderLogViewerPanel(logViewerRunID);
  }
  const timelineRunID = timelineRunIDFromRoute();
  if (timelineRunID) {
    return renderTimelinePanel(timelineRunID);
  }
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

function renderTimelinePanel(runID) {
  return `
    <section class="panel timeline-panel" tabindex="-1" data-timeline-panel data-run-id="${escapeText(runID)}">
      <div class="timeline-heading">
        <h2>Run Timeline</h2>
        <span class="timeline-status" data-timeline-status></span>
      </div>
      <div class="timeline-viewport" data-timeline-viewport>
        <div class="timeline-spacer" data-timeline-spacer></div>
      </div>
    </section>
  `;
}

function login(event) {
  event.preventDefault();
  const data = new FormData(event.currentTarget);
  state.token = String(data.get('apiKey') || '');
  sessionStorage.setItem('agentpaas.dashboard.token', state.token);
  loadResources();
}

function render() {
  teardownTimelineIfRouteChanged();
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
  const runID = timelineRunIDFromRoute();
  if (runID) {
    mountTimeline(runID);
  }
  const logRunID = logViewerRunIDFromRoute();
  if (logRunID) {
    mountLogViewer(logRunID);
  }
}

window.addEventListener('hashchange', setRoute);
render();
loadResources();

function renderLogViewerPanel(runID) {
  return `
    <section class="panel log-viewer-panel" tabindex="-1" data-log-viewer-panel data-run-id="${escapeText(runID)}">
      <div class="timeline-heading">
        <h2>Logs</h2>
        <span class="timeline-status" data-log-viewer-status></span>
      </div>
      <div class="timeline-viewport" data-log-viewer-viewport>
        <table class="log-viewer-table">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Severity</th>
              <th scope="col">Body</th>
              <th scope="col">Attributes</th>
              <th scope="col">Resource</th>
            </tr>
          </thead>
          <tbody data-log-viewer-body></tbody>
        </table>
      </div>
    </section>
  `;
}

function mountLogViewer(runID) {
  const panel = app.querySelector('[data-log-viewer-panel]');
  if (!panel) {
    return;
  }
  if (!state.token) {
    state.logViewer.rows = [];
    setLogViewerStatus(panel, 'API key required');
    renderLogViewerRows(panel);
    return;
  }
  if (state.logViewer.runID !== runID) {
    state.logViewer.runID = runID;
    state.logViewer.rows = [];
    state.logViewer.error = '';
    loadLogViewerRows(runID, panel);
    return;
  }
  renderLogViewerRows(panel);
}

async function loadLogViewerRows(runID, panel) {
  state.logViewer.loading = true;
  setLogViewerStatus(panel, 'Loading');
  renderLogViewerRows(panel);
  try {
    const response = await fetch(`/api/runs/${encodeURIComponent(runID)}/logs`, {
      headers: authHeader()
    });
    if (!response.ok) {
      throw new Error(`logs request failed: ${response.status}`);
    }
    const rows = await response.json();
    state.logViewer.rows = Array.isArray(rows) ? rows : [];
    state.logViewer.error = '';
  } catch (error) {
    state.logViewer.rows = [];
    state.logViewer.error = 'Unable to load logs';
  } finally {
    state.logViewer.loading = false;
    renderLogViewerRows(panel);
  }
}

function renderLogViewerRows(panel) {
  const body = panel.querySelector('[data-log-viewer-body]');
  if (!body) {
    return;
  }
  body.replaceChildren();
  if (state.logViewer.loading) {
    body.appendChild(createLogViewerMessageRow('Loading'));
    return;
  }
  if (state.logViewer.error) {
    body.appendChild(createLogViewerMessageRow(state.logViewer.error));
    setLogViewerStatus(panel, state.logViewer.error);
    return;
  }
  if (!state.logViewer.rows.length) {
    body.appendChild(createLogViewerMessageRow('No logs'));
    setLogViewerStatus(panel, 'No logs');
    return;
  }
  state.logViewer.rows.forEach((entry) => {
    body.appendChild(createLogViewerRow(entry));
  });
  setLogViewerStatus(panel, `${state.logViewer.rows.length} logs`);
}

function createLogViewerMessageRow(message) {
  const row = document.createElement('tr');
  const cell = document.createElement('td');
  cell.colSpan = 5;
  cell.className = 'empty';
  cell.textContent = message;
  row.appendChild(cell);
  return row;
}

function createLogViewerRow(entry) {
  const row = document.createElement('tr');
  row.appendChild(createLogViewerCell(formatTimelineTime(entry.timestamp)));
  row.appendChild(createSeverityCell(entry.severity));
  const bodyCell = createLogViewerCell(entry.body || '');
  if (entry.truncated) {
    const indicator = document.createElement('span');
    indicator.className = 'timeline-status';
    indicator.textContent = ' truncated';
    bodyCell.appendChild(indicator);
  }
  row.appendChild(bodyCell);
  row.appendChild(createLogViewerCell(formatLogViewerMap(entry.attributes)));
  row.appendChild(createLogViewerCell(formatLogViewerMap(entry.resource)));
  return row;
}

function createLogViewerCell(value) {
  const cell = document.createElement('td');
  cell.textContent = value || '';
  return cell;
}

function createSeverityCell(value) {
  const cell = createLogViewerCell(value);
  const severity = String(value || '').toLowerCase();
  if (severity.includes('error')) {
    cell.style.color = '#b42318';
  } else if (severity.includes('warn')) {
    cell.style.color = '#a15c00';
  }
  return cell;
}

function formatLogViewerMap(value) {
  if (!value || typeof value !== 'object') {
    return '';
  }
  return JSON.stringify(value);
}

function setLogViewerStatus(panel, text) {
  const status = panel.querySelector('[data-log-viewer-status]');
  if (status) {
    status.textContent = text;
  }
}

function logViewerRunIDFromRoute() {
  const match = state.route.match(/^#\/runs\/([^/]+)\/logs$/);
  if (!match) {
    return '';
  }
  return decodeURIComponent(match[1]);
}

function timelineRunIDFromRoute() {
  const match = state.route.match(/^#\/runs\/([^/]+)\/timeline$/);
  if (!match) {
    return '';
  }
  return decodeURIComponent(match[1]);
}

function teardownTimelineIfRouteChanged() {
  const nextRunID = timelineRunIDFromRoute();
  if (state.timeline.controller && state.timeline.runID !== nextRunID) {
    state.timeline.controller.abort();
    state.timeline.controller = null;
  }
  if (state.timeline.reconnectTimer && state.timeline.runID !== nextRunID) {
    window.clearTimeout(state.timeline.reconnectTimer);
    state.timeline.reconnectTimer = 0;
  }
  if (!nextRunID && state.timeline.rows.length > 0) {
    state.timeline.rows = [];
    state.timeline.lastEventID = '';
    state.timeline.status = 'idle';
  }
}

function mountTimeline(runID) {
  const panel = app.querySelector('[data-timeline-panel]');
  if (!panel) {
    return;
  }
  const viewport = panel.querySelector('[data-timeline-viewport]');
  viewport.addEventListener('scroll', () => renderTimelineRows(panel));
  if (state.timeline.runID !== runID) {
    state.timeline.runID = runID;
    state.timeline.rows = [];
    state.timeline.lastEventID = '';
    state.timeline.status = 'connecting';
  }
  renderTimelineRows(panel);
  if (!state.token) {
    setTimelineStatus(panel, 'API key required');
    return;
  }
  if (!state.timeline.controller) {
    connectTimeline(runID, panel);
  }
}

async function connectTimeline(runID, panel) {
  if (state.timeline.controller) {
    state.timeline.controller.abort();
  }
  const controller = new AbortController();
  state.timeline.controller = controller;
  state.timeline.status = 'connecting';
  setTimelineStatus(panel, 'Connecting');
  const headers = authHeader();
  if (state.timeline.lastEventID) {
    headers['Last-Event-ID'] = state.timeline.lastEventID;
  }
  try {
    const response = await fetch(`/api/runs/${encodeURIComponent(runID)}/timeline`, {
      headers,
      signal: controller.signal
    });
    if (!response.ok || !response.body) {
      throw new Error(`timeline stream failed: ${response.status}`);
    }
    state.timeline.status = 'live';
    setTimelineStatus(panel, 'Live');
    await readTimelineStream(response.body, panel);
  } catch (error) {
    if (controller.signal.aborted) {
      return;
    }
    state.timeline.status = 'reconnecting';
    setTimelineStatus(panel, 'Reconnecting');
    state.timeline.controller = null;
    state.timeline.reconnectTimer = window.setTimeout(() => {
      state.timeline.reconnectTimer = 0;
      if (timelineRunIDFromRoute() === runID) {
        connectTimeline(runID, panel);
      }
    }, 1000);
  }
}

async function readTimelineStream(body, panel) {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const result = await reader.read();
    if (result.done) {
      break;
    }
    buffer += decoder.decode(result.value, { stream: true });
    let boundary = buffer.indexOf('\n\n');
    while (boundary !== -1) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      handleTimelineSSE(chunk, panel);
      boundary = buffer.indexOf('\n\n');
    }
  }
}

function handleTimelineSSE(chunk, panel) {
  const message = parseSSEChunk(chunk);
  if (!message.event || !message.data) {
    return;
  }
  if (message.id) {
    state.timeline.lastEventID = message.id;
  }
  let payload;
  try {
    payload = JSON.parse(message.data);
  } catch (error) {
    return;
  }
  if (message.event === 'span_batch' && Array.isArray(payload.events)) {
    payload.events.forEach(addTimelineRow);
  } else {
    addTimelineRow(payload);
  }
  renderTimelineRows(panel);
  if (isTerminalTimelineEvent(payload)) {
    setTimelineStatus(panel, 'Complete');
  }
}

function parseSSEChunk(chunk) {
  const message = { id: '', event: '', data: '' };
  chunk.split('\n').forEach((line) => {
    if (line.startsWith('id: ')) {
      message.id = line.slice(4);
    } else if (line.startsWith('event: ')) {
      message.event = line.slice(7);
    } else if (line.startsWith('data: ')) {
      message.data += message.data ? `\n${line.slice(6)}` : line.slice(6);
    }
  });
  return message;
}

function addTimelineRow(event) {
  if (!event || event.type === 'heartbeat') {
    return;
  }
  state.timeline.rows.push(event);
}

function isTerminalTimelineEvent(event) {
  if (!event || event.type !== 'run_event' || !event.data) {
    return false;
  }
  return ['run_succeeded', 'run_failed', 'run_cancelled'].includes(event.data.type);
}

function renderTimelineRows(panel) {
  const viewport = panel.querySelector('[data-timeline-viewport]');
  const spacer = panel.querySelector('[data-timeline-spacer]');
  if (!viewport || !spacer) {
    return;
  }
  const rows = state.timeline.rows;
  spacer.style.height = `${rows.length * TIMELINE_ROW_HEIGHT}px`;
  const visibleCount = Math.ceil(viewport.clientHeight / TIMELINE_ROW_HEIGHT) + TIMELINE_BUFFER * 2;
  const start = Math.max(0, Math.floor(viewport.scrollTop / TIMELINE_ROW_HEIGHT) - TIMELINE_BUFFER);
  const end = Math.min(rows.length, start + visibleCount);
  spacer.replaceChildren();
  for (let index = start; index < end; index += 1) {
    spacer.appendChild(createTimelineRow(rows[index], index));
  }
  setTimelineStatus(panel, timelineStatusText(rows.length));
}

function createTimelineRow(event, index) {
  const row = document.createElement('div');
  row.className = `timeline-row timeline-${event.type || 'event'}`;
  row.style.transform = `translateY(${index * TIMELINE_ROW_HEIGHT}px)`;

  const icon = document.createElement('span');
  icon.className = 'timeline-icon';
  icon.textContent = timelineIcon(event.type);
  row.appendChild(icon);

  const main = document.createElement('div');
  main.className = 'timeline-main';
  const title = document.createElement('strong');
  title.textContent = timelineTitle(event);
  const detail = document.createElement('span');
  detail.textContent = timelineDetail(event);
  main.appendChild(title);
  main.appendChild(detail);
  row.appendChild(main);

  const time = document.createElement('time');
  time.dateTime = event.timestamp || '';
  time.textContent = formatTimelineTime(event.timestamp);
  row.appendChild(time);
  return row;
}

function timelineIcon(type) {
  const icons = {
    llm_call: 'L',
    mcp_call: 'M',
    egress_allowed: 'E',
    egress_denied: '!',
    budget: 'B',
    audit: 'A',
    run_event: 'R'
  };
  return icons[type] || 'R';
}

function timelineTitle(event) {
  const data = event.data || {};
  switch (event.type) {
    case 'llm_call':
      return `${data.provider || 'LLM'} ${data.model || ''}`.trim();
    case 'mcp_call':
      return `${data.server || 'MCP'} ${data.tool || ''}`.trim();
    case 'egress_allowed':
      return `Allowed ${data.method || 'request'}`;
    case 'egress_denied':
      return `Denied ${data.method || 'request'}`;
    case 'budget':
      return `Budget ${data.type || 'marker'}`;
    case 'audit':
      return `Audit ${data.event_type || 'event'}`;
    case 'run_event':
      return data.type || 'Run event';
    default:
      return event.type || 'Timeline event';
  }
}

function timelineDetail(event) {
  const data = event.data || {};
  switch (event.type) {
    case 'llm_call':
      return `${data.input_tokens || 0} in / ${data.output_tokens || 0} out`;
    case 'mcp_call':
      return data.status || '';
    case 'egress_allowed':
    case 'egress_denied':
      return [data.destination || '', data.deny_reason || data.status_code || ''].filter(Boolean).join(' ');
    case 'budget':
      return `${data.current || 0} / ${data.limit || 0}`;
    case 'audit':
      return [data.actor || '', data.seq || ''].filter(Boolean).join(' ');
    case 'run_event':
      return JSON.stringify(data.data || {});
    default:
      return '';
  }
}

function formatTimelineTime(value) {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  return date.toLocaleTimeString();
}

function setTimelineStatus(panel, text) {
  const status = panel.querySelector('[data-timeline-status]');
  if (status) {
    status.textContent = text;
  }
}

function timelineStatusText(count) {
  if (!state.token) {
    return 'API key required';
  }
  if (state.timeline.status === 'reconnecting') {
    return 'Reconnecting';
  }
  if (state.timeline.status === 'connecting') {
    return 'Connecting';
  }
  return `${count} events`;
}
