const $ = selector => document.querySelector(selector);
const state = {config: null, items: [], sources: [], downloads: [], type: 'all', source: 'all', query: ''};
const folderState = {target: null, current: null, parent: null};
const pageRoutes = {discover: '/inicio', downloads: '/descargas', servers: '/servidores', settings: '/ajustes'};
const routePages = Object.fromEntries(Object.entries(pageRoutes).map(([page, route]) => [route, page]));

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

async function api(path, options = {}) {
  const response = await fetch(path, options);
  const contentType = response.headers.get('content-type') || '';
  const payload = contentType.includes('application/json') ? await response.json() : await response.text();
  if (!response.ok) throw new Error(payload.error || payload || `Error ${response.status}`);
  return payload;
}

function showToast(message) {
  const toast = $('#toast');
  toast.textContent = message;
  toast.classList.remove('hidden');
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => toast.classList.add('hidden'), 3500);
}

function syncSetupMode() {
  const isNode = $('#setup-form').elements.mode.value === 'node';
  $('#join-fields').classList.toggle('hidden', !isNode);
  $('#setup-form').elements.coordinator.required = isNode;
  $('#setup-form').elements.networkKey.required = isNode;
  $('#setup-form').elements.publicUrl.required = isNode;
  document.querySelectorAll('.mode-option').forEach(option => option.classList.toggle('selected', option.querySelector('input').checked));
}

$('#setup-form').addEventListener('change', event => {
  if (event.target.name === 'mode') syncSetupMode();
});

$('#setup-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  $('#setup-error').textContent = '';
  button.disabled = true;
  try {
    state.config = await api('/api/v1/setup', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.fromEntries(new FormData(form)))
    });
    $('#setup').classList.add('hidden');
    startApplication();
  } catch (error) {
    $('#setup-error').textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

$('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  $('#login-error').textContent = '';
  button.disabled = true;
  try {
    const endpoint = state.config.needsClaim ? '/api/v1/claim' : '/api/v1/login';
    state.config = await api(endpoint, {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({password: form.elements.password.value})
    });
    form.reset();
    $('#login').classList.add('hidden');
    startApplication();
  } catch (error) {
    $('#login-error').textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

function startApplication() {
  const config = state.config;
  $('#application').classList.remove('hidden');
  $('#sidebar-node').textContent = config.nodeName;
  $('#sidebar-role').textContent = config.mode === 'coordinator' ? 'Coordinador' : 'Nodo conectado';
  $('#node-initial').textContent = (config.nodeName || 'N').trim().charAt(0).toUpperCase();
  $('#destination-node').textContent = config.nodeName;
  $('#local-node-name').textContent = config.nodeName;
  $('#local-node-url').textContent = config.publicUrl || 'Solo red local';
  $('#local-library-count').textContent = `${(config.libraries || []).length} bibliotecas`;
  $('#jellyfin-form').elements.url.value = config.jellyfinUrl || '';
  if (config.mode === 'coordinator' && config.inviteCode) {
    $('#invite-card').classList.remove('hidden');
    $('#invite-code').textContent = config.inviteCode;
  }
  if (config.mode === 'node') {
    $('#reconnect-form').classList.remove('hidden');
    $('#reconnect-form').elements.coordinator.value = config.coordinator || '';
  }
  renderPeers();
  loadCatalog();
  loadDownloads();
  loadStorage();
  clearInterval(startApplication.downloadTimer);
  startApplication.downloadTimer = setInterval(loadDownloads, 2500);
  if (['/', '/acceso', '/configuracion'].includes(location.pathname)) {
    history.replaceState({}, '', '/inicio');
  }
  renderRoute();
}

async function loadCatalog() {
  const grid = $('#catalog-grid');
  grid.replaceChildren(element('div', 'empty-panel', 'Leyendo los catálogos conectados…'));
  $('#catalog-message').classList.add('hidden');
  try {
    const result = await api('/api/v1/discover');
    state.items = result.items || [];
    state.sources = (result.sources || []).filter(source => source.id !== state.config.nodeId);
    state.config.peers = state.sources;
    renderPeers();
    renderSourceOptions();
    if (result.errors?.length) {
      $('#catalog-message').textContent = result.errors.join(' · ');
      $('#catalog-message').classList.remove('hidden');
    }
    renderCatalog();
    openContentFromRoute();
  } catch (error) {
    grid.replaceChildren(element('div', 'empty-panel', error.message));
  }
}

function renderSourceOptions() {
  const select = $('#source-filter');
  const previous = select.value;
  select.replaceChildren();
  const all = element('option', '', 'Todos los servidores');
  all.value = 'all';
  select.append(all);
  state.sources.forEach(source => {
    const option = element('option', '', source.name);
    option.value = source.id;
    select.append(option);
  });
  select.value = [...select.options].some(option => option.value === previous) ? previous : 'all';
  state.source = select.value;
  const count = state.sources.length;
  $('#peer-summary').textContent = count ? `${count} servidor${count === 1 ? '' : 'es'} conectado${count === 1 ? '' : 's'}` : 'Sin servidores remotos';
}

function visibleItems() {
  const query = state.query.toLocaleLowerCase('es');
  return state.items.filter(item => {
    if (item.sourceId === state.config.nodeId) return false;
    if (state.source !== 'all' && item.sourceId !== state.source) return false;
    if (state.type !== 'all' && item.type !== state.type) return false;
    if (query && !`${item.name} ${item.seriesName || ''}`.toLocaleLowerCase('es').includes(query)) return false;
    return true;
  });
}

function renderCatalog() {
  const grid = $('#catalog-grid');
  grid.replaceChildren();
  const items = visibleItems();
  if (!state.sources.length) {
    grid.append(element('div', 'empty-panel', 'Todavía no hay otro servidor conectado. Crea una invitación desde Servidores y compártela con tu amigo.'));
    return;
  }
  if (!items.length) {
    grid.append(element('div', 'empty-panel', 'No hay contenido remoto que coincida con estos filtros.'));
    return;
  }
  items.forEach(item => grid.append(mediaCard(item)));
}

function mediaCard(item) {
  const card = element('article', 'media-card');
  card.tabIndex = 0;
  const poster = element('div', 'poster');
  if (item.hasImage) {
    const image = document.createElement('img');
    image.loading = 'lazy';
    image.alt = `Póster de ${item.name}`;
    image.src = `/api/v1/images/${encodeURIComponent(item.sourceId)}/${encodeURIComponent(item.id)}`;
    image.addEventListener('error', () => image.replaceWith(element('div', 'poster-fallback', item.name)));
    poster.append(image);
  } else {
    poster.append(element('div', 'poster-fallback', item.name));
  }
  poster.append(element('span', 'media-type', typeLabel(item.type)));
  card.append(poster);
  card.append(element('h3', '', item.type === 'Episode' && item.seriesName ? item.seriesName : item.name));
  const subtitle = item.type === 'Episode' ? episodeLabel(item) : [item.productionYear || '', item.sourceName].filter(Boolean).join(' · ');
  card.append(element('p', '', subtitle));
  const open = () => openDetail(item);
  card.addEventListener('click', open);
  card.addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') open(); });
  return card;
}

function openDetail(item, updateRoute = true) {
  if (updateRoute) {
    history.pushState({}, '', `/contenido/${encodeURIComponent(item.sourceId)}/${encodeURIComponent(item.id)}`);
  }
  const root = $('#detail-content');
  root.replaceChildren();
  const layout = element('div', 'detail-layout');
  if (item.hasImage) {
    const image = element('img', 'detail-poster');
    image.alt = `Póster de ${item.name}`;
    image.src = `/api/v1/images/${encodeURIComponent(item.sourceId)}/${encodeURIComponent(item.id)}`;
    layout.append(image);
  } else {
    layout.append(element('div', 'detail-poster poster-fallback', item.name));
  }
  const body = element('div', 'detail-body');
  body.append(element('span', 'eyebrow', `${typeLabel(item.type).toUpperCase()} · ${item.sourceName.toUpperCase()}`));
  body.append(element('h2', '', item.type === 'Episode' && item.seriesName ? item.seriesName : item.name));
  const metadata = item.type === 'Episode' ? `${episodeLabel(item)} · ${formatBytes(item.size)}` : `${item.productionYear || 'Año desconocido'} · ${formatBytes(item.size)}`;
  body.append(element('p', 'detail-meta', metadata));
  if (item.overview) body.append(element('p', 'detail-overview', item.overview));
  const source = element('div', 'detail-source');
  source.append(element('span', '', `Origen: ${item.sourceName}`), element('strong', '', `Destino: ${state.config.nodeName}`));
  body.append(source);
  if (item.type === 'Series') {
    const episodes = state.items.filter(candidate => candidate.type === 'Episode' && candidate.sourceId === item.sourceId && candidate.seriesId === item.id);
    const list = element('div', 'episode-list');
    if (!episodes.length) list.append(element('p', 'detail-overview', 'Este servidor no ha publicado episodios para la serie.'));
    episodes.sort((a, b) => a.seasonNumber - b.seasonNumber || a.episodeNumber - b.episodeNumber).forEach(episode => list.append(episodeRow(episode)));
    body.append(list);
  } else {
    const actions = element('div', 'detail-actions');
    const button = element('button', 'button primary', 'Descargar en este servidor');
    button.addEventListener('click', () => requestDownload(item, button));
    actions.append(button);
    body.append(actions);
  }
  layout.append(body);
  root.append(layout);
  $('#detail-dialog').showModal();
}

function openContentFromRoute() {
  const parts = location.pathname.split('/').filter(Boolean);
  if (parts[0] !== 'contenido' || parts.length !== 3 || !state.items.length) return;
  const sourceID = decodeURIComponent(parts[1]);
  const itemID = decodeURIComponent(parts[2]);
  const item = state.items.find(candidate => candidate.sourceId === sourceID && candidate.id === itemID);
  if (item && !$('#detail-dialog').open) openDetail(item, false);
}

function episodeRow(item) {
  const row = element('div', 'episode-row');
  const info = element('div');
  info.append(element('strong', '', `${episodeLabel(item)} · ${item.name}`), element('small', '', formatBytes(item.size)));
  const button = element('button', '', 'Descargar');
  button.addEventListener('click', () => requestDownload(item, button));
  row.append(info, button);
  return row;
}

async function requestDownload(item, button) {
  button.disabled = true;
  try {
    await api('/api/v1/downloads', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({sourceId: item.sourceId, itemId: item.id})
    });
    showToast(`${item.name} se ha añadido a la cola.`);
    loadDownloads();
  } catch (error) {
    showToast(error.message);
  } finally {
    button.disabled = false;
  }
}

async function loadDownloads() {
  if (!state.config) return;
  try {
    state.downloads = await api('/api/v1/downloads');
    renderDownloads();
  } catch (_) {}
}

function renderDownloads() {
  const list = $('#download-list');
  list.replaceChildren();
  const active = state.downloads.filter(job => !['completed', 'failed'].includes(job.status)).length;
  $('#download-badge').textContent = active;
  $('#download-badge').classList.toggle('hidden', active === 0);
  if (!state.downloads.length) {
    list.append(element('div', 'empty-panel', 'Todavía no has solicitado ninguna descarga.'));
    return;
  }
  [...state.downloads].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).forEach(job => {
    const row = element('article', 'download-row');
    const title = element('div');
    title.append(element('strong', '', job.name || 'Preparando contenido…'), element('small', '', `${job.sourceName} → ${state.config.nodeName}`));
    const progressBox = element('div');
    const progress = element('div', 'progress');
    const bar = element('i');
    const percent = job.bytesTotal > 0 ? Math.min(100, job.bytesDone / job.bytesTotal * 100) : 0;
    bar.style.width = `${percent}%`;
    progress.append(bar);
    progressBox.append(progress, element('small', '', `${formatBytes(job.bytesDone)} de ${formatBytes(job.bytesTotal)}`));
    const status = element('span', `status ${job.status}`, statusLabel(job.status));
    if (job.error) status.title = job.error;
    row.append(title, progressBox, status);
    list.append(row);
  });
}

function renderPeers() {
  const peers = state.config.peers || [];
  const list = $('#peer-list');
  list.replaceChildren();
  if (!peers.length) {
    list.append(element('div', 'empty-panel', 'No hay servidores remotos registrados.'));
    return;
  }
  peers.forEach(peer => {
    const row = element('article', 'peer-row');
    const info = element('div');
    info.append(element('strong', '', peer.name), element('small', '', peer.url));
    const action = state.config.mode === 'coordinator' ? element('button', 'text-button', 'Eliminar') : element('span', 'status completed', 'Registrado');
    if (state.config.mode === 'coordinator') action.addEventListener('click', () => removePeer(peer, action));
    row.append(info, element('small', '', 'Catálogo y transferencias'), action);
    list.append(row);
  });
}

function showPage(page) {
  document.querySelectorAll('.nav-item').forEach(item => item.classList.toggle('active', item.dataset.page === page));
  document.querySelectorAll('.page').forEach(page => page.classList.add('hidden'));
  $(`#page-${page}`).classList.remove('hidden');
  if (page === 'downloads') loadDownloads();
  if (page === 'settings') loadStorage();
}

function renderRoute() {
  const isContent = location.pathname.startsWith('/contenido/');
  if (!isContent && !routePages[location.pathname]) {
    navigate('/inicio', true);
    return;
  }
  const page = isContent ? 'discover' : (routePages[location.pathname] || 'discover');
  showPage(page);
  if (!isContent && $('#detail-dialog').open) $('#detail-dialog').close();
  if (isContent) openContentFromRoute();
}

function navigate(route, replace = false) {
  history[replace ? 'replaceState' : 'pushState']({}, '', route);
  renderRoute();
}

document.querySelectorAll('.nav-item').forEach(link => link.addEventListener('click', event => {
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  navigate(link.getAttribute('href'));
}));

window.addEventListener('popstate', renderRoute);

document.querySelectorAll('.filter').forEach(button => button.addEventListener('click', () => {
  document.querySelectorAll('.filter').forEach(item => item.classList.toggle('active', item === button));
  state.type = button.dataset.type;
  renderCatalog();
}));

$('#search-input').addEventListener('input', event => { state.query = event.target.value.trim(); renderCatalog(); });
$('#source-filter').addEventListener('change', event => { state.source = event.target.value; renderCatalog(); });
$('#refresh-catalog').addEventListener('click', loadCatalog);
function closeDetail() {
  $('#detail-dialog').close();
  if (location.pathname.startsWith('/contenido/')) navigate('/inicio', true);
}

$('#close-detail').addEventListener('click', closeDetail);
$('#detail-dialog').addEventListener('click', event => { if (event.target === $('#detail-dialog')) closeDetail(); });
$('#detail-dialog').addEventListener('cancel', event => { event.preventDefault(); closeDetail(); });
$('#copy-invite').addEventListener('click', async () => {
  await navigator.clipboard.writeText(state.config.inviteCode);
  showToast('Código copiado.');
});

$('#reconnect-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  $('#reconnect-error').textContent = '';
  button.disabled = true;
  try {
    state.config = await api('/api/v1/network/reconnect', {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.fromEntries(new FormData(form)))
    });
    form.elements.networkKey.value = '';
    renderPeers();
    showToast('Conexión reparada correctamente.');
    loadCatalog();
  } catch (error) {
    $('#reconnect-error').textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

async function removePeer(peer, button) {
  if (!confirm(`¿Eliminar la conexión con ${peer.name}?`)) return;
  button.disabled = true;
  try {
    state.config = await api(`/api/v1/peers/${encodeURIComponent(peer.id)}`, {method: 'DELETE'});
    renderPeers();
    loadCatalog();
    showToast(`${peer.name} se ha eliminado.`);
  } catch (error) {
    showToast(error.message);
    button.disabled = false;
  }
}

$('#jellyfin-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  $('#jellyfin-error').textContent = '';
  button.disabled = true;
  try {
    state.config = await api('/api/v1/jellyfin', {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.fromEntries(new FormData(form)))
    });
    form.elements.apiKey.value = '';
    $('#local-library-count').textContent = `${(state.config.libraries || []).length} bibliotecas`;
    showToast('Conexión actualizada.');
    loadCatalog();
  } catch (error) {
    $('#jellyfin-error').textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

async function loadStorage() {
  try {
    const storage = await api('/api/v1/storage');
    $('#storage-label').textContent = storage.label;
    $('#storage-root').textContent = storage.root;
    const form = $('#storage-form');
    form.elements.moviesDir.value = storage.moviesDir;
    form.elements.seriesDir.value = storage.seriesDir;
    form.elements.downloadDir.value = storage.downloadDir;
  } catch (error) {
    $('#storage-error').textContent = error.message;
  }
}

$('#storage-form').addEventListener('submit', async event => {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type=submit]');
  $('#storage-error').textContent = '';
  button.disabled = true;
  try {
    const storage = await api('/api/v1/storage', {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.fromEntries(new FormData(form)))
    });
    form.elements.moviesDir.value = storage.moviesDir;
    form.elements.seriesDir.value = storage.seriesDir;
    form.elements.downloadDir.value = storage.downloadDir;
    showToast('Rutas comprobadas y guardadas.');
  } catch (error) {
    $('#storage-error').textContent = error.message;
  } finally {
    button.disabled = false;
  }
});

async function browseFolder(path) {
  const list = $('#folder-list');
  list.replaceChildren(element('div', 'empty-panel', 'Leyendo carpetas…'));
  try {
    const listing = await api(`/api/v1/storage/browse?path=${encodeURIComponent(path || '')}`);
    folderState.current = listing.current;
    folderState.parent = listing.parent || null;
    $('#folder-current').textContent = listing.current;
    $('#folder-up').disabled = !folderState.parent;
    list.replaceChildren();
    if (!listing.directories.length) list.append(element('div', 'empty-panel', 'Esta carpeta no contiene otras carpetas.'));
    listing.directories.forEach(directory => {
      const button = element('button', 'folder-item', directory.split('/').filter(Boolean).pop() || directory);
      button.type = 'button';
      button.addEventListener('click', () => browseFolder(directory));
      list.append(button);
    });
  } catch (error) {
    list.replaceChildren(element('div', 'empty-panel', error.message));
  }
}

document.querySelectorAll('[data-browse]').forEach(button => button.addEventListener('click', () => {
  folderState.target = button.dataset.browse;
  const requested = $('#storage-form').elements[folderState.target].value;
  $('#folder-dialog').showModal();
  browseFolder(requested);
}));

$('#folder-up').addEventListener('click', () => { if (folderState.parent) browseFolder(folderState.parent); });
$('#close-folder').addEventListener('click', () => $('#folder-dialog').close());
$('#folder-dialog').addEventListener('cancel', event => { event.preventDefault(); $('#folder-dialog').close(); });
$('#select-folder').addEventListener('click', () => {
  if (folderState.target && folderState.current) {
    $('#storage-form').elements[folderState.target].value = folderState.current;
  }
  $('#folder-dialog').close();
});

function typeLabel(type) { return ({Movie: 'Película', Series: 'Serie', Episode: 'Episodio'})[type] || type; }
function episodeLabel(item) { return `T${String(item.seasonNumber || 0).padStart(2, '0')} · E${String(item.episodeNumber || 0).padStart(2, '0')}`; }
function formatBytes(value) {
  if (!value) return 'Tamaño desconocido';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`;
}
function statusLabel(status) { return ({queued: 'En cola', downloading: 'Descargando', moving: 'Organizando', completed: 'Completada', failed: 'Fallida'})[status] || status; }

api('/api/v1/config')
  .then(config => {
    state.config = config;
    if (!config.configured) {
      history.replaceState({}, '', '/configuracion');
      $('#setup').classList.remove('hidden');
    } else if (!config.authenticated) {
      if (location.pathname === '/') history.replaceState({}, '', '/acceso');
      if (config.needsClaim) $('#login-copy').textContent = 'Esta instalación es anterior al acceso protegido. Crea ahora su contraseña administrativa.';
      $('#login').classList.remove('hidden');
    } else {
      startApplication();
    }
  })
  .catch(error => {
    $('#setup').classList.remove('hidden');
    $('#setup-error').textContent = error.message;
  });
