import './style.css';

import {EventsOn, EventsEmit, BrowserOpenURL} from '../wailsjs/runtime/runtime';
import {TelemetrySnapshot, GetHistory, GetLatestImageInfo, GetSettings, SaveSettings, StartRecording, StopRecording, ClearCache, GetSessionPath, GetPayloadName, GetTileServerURL, PreloadTiles, EstimateTiles, UpdateRateLimit, CancelPreload, GetVersion, SelectDirectory} from '../wailsjs/go/main/App';

// ---- Tab switching ----
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
        btn.classList.add('active');
        document.getElementById('tab-' + btn.dataset.tab).classList.add('active');

        if (btn.dataset.tab === 'telemetry') {
            setTimeout(() => {
                if (window.telemetryMap) window.telemetryMap.invalidateSize();
            }, 200);
        }
        if (btn.dataset.tab === 'images') {
            viewingHistory = false;
            document.getElementById('btn-back-live').classList.add('hidden');
            pollImage(); // Immediately show latest received image
        }
    });
});

// ---- Hamburger menu ----
const hamburgerBtn = document.getElementById('hamburger');
const hamburgerMenu = document.getElementById('hamburger-menu');
hamburgerBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    hamburgerMenu.classList.toggle('hidden');
});
document.addEventListener('click', (e) => {
    if (!hamburgerMenu.contains(e.target) && e.target !== hamburgerBtn) {
        hamburgerMenu.classList.add('hidden');
    }
});

// ---- Image display ----
const canvas = document.getElementById('image-canvas');
const ctx = canvas.getContext('2d');

function displayImage(base64Data) {
    if (!base64Data) return;
    const img = new Image();
    img.onload = () => {
        canvas.width = img.width;
        canvas.height = img.height;
        ctx.drawImage(img, 0, 0);
    };
    img.src = 'data:image/jpeg;base64,' + base64Data;
}

// ---- Telemetry map ----
let mapInitialized = false;
function initMap() {
    if (mapInitialized) return;
    const el = document.getElementById('telem-map');
    if (!el) return;
    mapInitialized = true;

    const map = L.map('telem-map', {
        center: [0, 0],
        zoom: 2
    });
    window.telemetryMap = map;

    const tileURL = window.tileServerURL || '';
    if (tileURL) {
        L.tileLayer(tileURL + '/tiles/{z}/{x}/{y}.png', {maxZoom: 19, attribution: 'OSM'}).addTo(map);
    } else {
        L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {maxZoom: 19, attribution: 'OSM'}).addTo(map);
    }

    // Bigger marker with popup
    window.telemMarker = L.circleMarker([0, 0], {
        radius: 8,
        color: '#ff4444',
        fillColor: '#ff4444',
        fillOpacity: 0.6,
        weight: 3
    }).addTo(map);
    window.telemMarker.bindPopup('Lat: --\nLon: --\nAlt: --');

    // Thicker trail polyline
    window.trailPoints = [];
    window.trailPolyline = L.polyline([], {
        color: '#44aaff',
        weight: 3,
        opacity: 0.8
    }).addTo(map);
}

// ---- Charts (cap at 100) ----
const MAX_CHART = 100;
const charts = {};
const lastChartVals = {battery: null, tempInt: null, tempExt: null, altitude: null};

['battery', 'tempInt', 'tempExt', 'altitude'].forEach(id => {
    const el = document.getElementById('chart-' + id);
    charts[id] = new Chart(el, {
        type: 'line',
        data: {labels: [], datasets: [{label: id, data: [], borderColor: ['orange','red','blue','green'][['battery','tempInt','tempExt','altitude'].indexOf(id)], tension: 0.2}]},
        options: {animation: false, responsive: true, maintainAspectRatio: false}
    });
});

function pushChart(id, val) {
    if (lastChartVals[id] === val) return;
    lastChartVals[id] = val;

    const c = charts[id];
    if (!c) return;
    c.data.labels.push('');
    c.data.datasets[0].data.push(val);
    if (c.data.labels.length > MAX_CHART) {
        c.data.labels.shift();
        c.data.datasets[0].data.shift();
    }
    c.update();
}

// ---- Telemetry log (formatted console) ----
const logEl = document.getElementById('telem-log');
function appendLog(line) {
    const ts = new Date().toLocaleTimeString('en-GB', {hour12: false});
    logEl.textContent += `[${ts}] ${line}\n`;
    // Keep max 200 lines
    const lines = logEl.textContent.split('\n');
    if (lines.length > 200) {
        logEl.textContent = lines.slice(-200).join('\n');
    }
    logEl.scrollTop = logEl.scrollHeight;
}

// ---- Resizable telemetry sections (map | charts+console) ----
function makeVerticalResizer(splitterId, topId, bottomId, topMin, bottomMin) {
    const splitter = document.getElementById(splitterId);
    const top = document.getElementById(topId);
    const bottom = document.getElementById(bottomId);
    let dragging = false, startY = 0, startTopPx = 0, startBotPx = 0;

    function apply(topPx, botPx) {
        top.style.flex = 'none';
        top.style.height = topPx + 'px';
        bottom.style.flex = 'none';
        bottom.style.height = botPx + 'px';
        if (window.telemetryMap) window.telemetryMap.invalidateSize();
    }

    splitter.addEventListener('mousedown', (e) => {
        dragging = true;
        startY = e.clientY;
        startTopPx = top.getBoundingClientRect().height;
        startBotPx = bottom.getBoundingClientRect().height;
        document.body.style.cursor = 'row-resize';
        document.body.style.userSelect = 'none';
        e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
        if (!dragging) return;
        const dy = e.clientY - startY;
        const topPx = Math.max(topMin, startTopPx + dy);
        const botPx = Math.max(bottomMin, startBotPx - dy);
        apply(topPx, botPx);
    });

    document.addEventListener('mouseup', () => {
        if (dragging) {
            dragging = false;
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
        }
    });
}
makeVerticalResizer('telem-splitter-map', 'telem-map', 'telem-bottom-wrap', 120, 100);


// ---- Poll telemetry ----
let firstMap = true;

async function pollTelemetry() {
    try {
        const raw = await TelemetrySnapshot();
        const d = JSON.parse(raw);

        // Titlebar
        document.getElementById('status-uptime').textContent = 'Mission: ' + (d.uptime || '0s');
        const recEl = document.getElementById('status-recording');
        if (d.recording) { recEl.textContent = 'REC'; recEl.className = 'rec-on'; recEl.classList.remove('hidden'); }
        else { recEl.classList.add('hidden'); }

        const pn = d.payloadName || '';
        document.getElementById('status-payload').textContent = pn ? 'Payload: ' + pn : '';

        // Statistics tab
        document.getElementById('info-session').textContent = await GetSessionPath();
        document.getElementById('info-payload').textContent = d.payloadName || '--';
        document.getElementById('info-images').textContent = d.total || 0;
        const tbody = document.querySelector('#apid-table tbody');
        if (tbody && d.apidList) {
            tbody.innerHTML = d.apidList.sort((a,b) => a.apid - b.apid).map(r => `<tr><td>${r.apid}</td><td>${r.type}</td><td>${r.port}</td><td>${r.packets}</td></tr>`).join('');
        }

        // Map
        initMap();
        const map = window.telemetryMap;
        const marker = window.telemMarker;

        if (d.core && map) {
            const c = d.core;
            if (firstMap && c.latitude && c.longitude) {
                map.setView([c.latitude, c.longitude], 13);
                firstMap = false;
            }
            if (c.latitude && c.longitude) {
                marker.setLatLng([c.latitude, c.longitude]);
                marker.setPopupContent(`Lat: ${c.latitude.toFixed(6)}\nLon: ${c.longitude.toFixed(6)}\nAlt: ${c.altitude}m`);
                window.trailPoints.push([c.latitude, c.longitude]);
                if (window.trailPoints.length > 2000) window.trailPoints.shift();
                window.trailPolyline.setLatLngs(window.trailPoints);
            }
            pushChart('battery', c.battVoltage || 0);
            pushChart('tempInt', c.tempInternal || 0);
            pushChart('tempExt', c.tempExternal || 0);
            pushChart('altitude', Number(c.altitude) || 0);
        }
    } catch (e) { /* ignore */ }
}

// ---- Back to live button ----
document.getElementById('btn-back-live').addEventListener('click', () => {
    viewingHistory = false;
    document.getElementById('btn-back-live').classList.add('hidden');
    pollImage();
});
// ---- Poll image ----
let viewingHistory = false;

async function pollImage() {
    if (viewingHistory) return; // Don't overwrite while browsing history
    try {
        const raw = await GetLatestImageInfo();
        const info = JSON.parse(raw);
        if (info.data) {
            document.getElementById('btn-back-live').classList.add('hidden');
            displayImage(info.data);
            document.getElementById('img-apid').textContent = 'APID: ' + (info.apid ?? '--');
            document.getElementById('img-id').textContent = 'Img: #' + info.imgId;
            document.getElementById('img-pkts').textContent = 'Pkts: ' + info.packetCount;
            document.getElementById('img-res').textContent = 'Res: ' + (info.width ?? '--') + 'x' + (info.height ?? '--');
            document.getElementById('img-res').title = 'Image resolution in pixels (width x height)';
            document.getElementById('img-quality').textContent = 'Quality: ' + (info.quality ?? '--');
            document.getElementById('img-quality').title = 'SSDV JPEG quality level (0-7, higher = better quality / larger file)';
        }
    } catch (e) { /* ignore */ }
}

// ---- Poll history ----
async function pollHistory() {
    try {
        const raw = await GetHistory();
        const entries = JSON.parse(raw);
        const list = document.getElementById('image-history-list');
        if (entries.length === 0) {
            list.innerHTML = '<div class="empty-msg">No images yet</div>';
            return;
        }
        document.getElementById('img-total').textContent = 'History: ' + entries.length;
        list.innerHTML = entries.map((e, i) =>
            `<div class="history-item" data-idx="${i}">
                <img src="data:image/jpeg;base64,${e.image}" class="history-thumb" loading="lazy"/>
                <span>#${e.imgId} APID ${e.apid} ${e.packetCount}pkts</span>
            </div>`
        ).join('');
        list.querySelectorAll('.history-item').forEach(el => {
            el.addEventListener('click', () => {
                viewingHistory = true;
                document.getElementById('btn-back-live').classList.remove('hidden');
                const idx = parseInt(el.dataset.idx);
                const e = entries[idx];
                if (e.image) displayImage(e.image);
                document.getElementById('img-id').textContent = 'Img: #' + e.imgId;
                document.getElementById('img-pkts').textContent = 'Pkts: ' + e.packetCount;
            });
        });
    } catch (e) { /* ignore */ }
}

// ---- Event listeners from Go ----
EventsOn('newImage', (base64Data) => {
    viewingHistory = false; // Resume live view when a new complete image arrives
    document.getElementById('btn-back-live').classList.add('hidden');
    displayImage(base64Data);
    pollHistory();
});

// ---- Console log from Go ----
EventsOn('log', (msg) => {
    appendLog(msg);
});

// ---- Settings (editable JSON) ----
const setJson = document.getElementById('set-json');
const setStatus = document.getElementById('set-status');
const setSaveDir = document.getElementById('set-save-dir');
const setMaxHistory = document.getElementById('set-max-history');
const setTileDir = document.getElementById('set-tile-dir');
const setTileConcurrency = document.getElementById('set-tile-concurrency');
let currentSettings = {};

document.getElementById('btn-save-settings').addEventListener('click', async () => {
    setStatus.textContent = 'Saving...';
    const json = setJson.value;
    // Merge simple fields
    let merged = JSON.parse(json);
    if (setSaveDir) merged.save_dir = setSaveDir.value;
    if (setMaxHistory) merged.max_history = parseInt(setMaxHistory.value) || 0;
    if (setTileDir) merged.tile_dir = setTileDir.value;
    if (setTileConcurrency) merged.tile_concurrency = parseInt(setTileConcurrency.value) || 2;
    const result = await SaveSettings(JSON.stringify(merged, null, 2));
    setStatus.textContent = result === 'ok' ? '✓ Saved' : '✗ ' + result;
    setTimeout(() => setStatus.textContent = '', 3000);
});

function populateSimpleFields(parsed) {
    if (setSaveDir) setSaveDir.value = parsed.save_dir || 'data';
    if (setMaxHistory) setMaxHistory.value = parsed.max_history || 50;
    if (setTileDir) setTileDir.value = parsed.tile_dir || 'tiles';
    if (setTileConcurrency) setTileConcurrency.value = parsed.tile_concurrency || 2;
}

document.getElementById('btn-choose-save-dir').addEventListener('click', async () => {
    const dir = await SelectDirectory("Select Save Directory");
    if (dir) {
        setSaveDir.value = dir;
    }
});

document.getElementById('btn-choose-tile-dir').addEventListener('click', async () => {
    const dir = await SelectDirectory("Select Tile Directory");
    if (dir) {
        setTileDir.value = dir;
    }
});

// ---- Hamburger menu actions ----
const statsStatus = document.getElementById('stats-status');

async function hamburgerActionStart() {
    const r = await StartRecording();
    statsStatus.textContent = r === 'ok' ? '✓ Recording' : r;
    hamburgerMenu.classList.add('hidden');
}
async function hamburgerActionStop() {
    const r = await StopRecording();
    statsStatus.textContent = r === 'ok' ? '✓ Stopped' : r;
    hamburgerMenu.classList.add('hidden');
}
async function hamburgerActionClear() {
    const r = await ClearCache();
    statsStatus.textContent = r === 'ok' ? '✓ Cache cleared' : r;
    hamburgerMenu.classList.add('hidden');
}

document.getElementById('btn-rec-start').addEventListener('click', hamburgerActionStart);
document.getElementById('btn-rec-stop').addEventListener('click', hamburgerActionStop);
document.getElementById('btn-clear-cache').addEventListener('click', hamburgerActionClear);

document.getElementById('btn-about').addEventListener('click', () => {
    document.getElementById('about-overlay').classList.remove('hidden');
    hamburgerMenu.classList.add('hidden');
});
document.getElementById('about-close').addEventListener('click', () => {
    document.getElementById('about-overlay').classList.add('hidden');
});
document.getElementById('about-overlay').addEventListener('click', (e) => {
    if (e.target === e.currentTarget) {
        document.getElementById('about-overlay').classList.add('hidden');
    }
});
const githubLink = document.getElementById('about-github');
if (githubLink) {
    githubLink.addEventListener('click', (e) => {
        e.preventDefault();
        BrowserOpenURL(githubLink.href);
    });
}

// ---- Tile picker map ----
let pickerMap = null;
let drawnItems = null;
function initTilePicker() {
    if (pickerMap) return;
    const el = document.getElementById('tile-picker-map');
    if (!el) return;
    pickerMap = L.map('tile-picker-map', {
        center: [51.5, -0.12],
        zoom: 6
    });
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {maxZoom: 19}).addTo(pickerMap);

    drawnItems = new L.FeatureGroup();
    pickerMap.addLayer(drawnItems);

    const drawControl = new L.Control.Draw({
        draw: { polygon: false, polyline: false, circle: false, circlemarker: false, marker: false },
        edit: { featureGroup: drawnItems }
    });
    pickerMap.addControl(drawControl);

    pickerMap.on(L.Draw.Event.CREATED, (e) => {
        drawnItems.clearLayers();
        drawnItems.addLayer(e.layer);
        updateTileEstimate();
    });
}

// Initialize picker when settings tab is shown
document.querySelector('[data-tab="settings"]').addEventListener('click', () => {
    setTimeout(initTilePicker, 200);
});

// ---- Tile estimate helper ----
async function updateTileEstimate() {
    const zoomMin = parseInt(document.getElementById('tile-zoom-min').value);
    const zoomMax = parseInt(document.getElementById('tile-zoom-max').value);
    const estimateEl = document.getElementById('tile-estimate');
    if (isNaN(zoomMin) || isNaN(zoomMax)) {
        estimateEl.textContent = '';
        return;
    }
    let bounds = null;
    if (drawnItems && drawnItems.getLayers().length > 0) {
        const layer = drawnItems.getLayers()[0];
        if (typeof layer.getBounds === 'function') {
            bounds = layer.getBounds();
        }
    }
    if (!bounds) {
        estimateEl.textContent = '';
        return;
    }
    const config = JSON.stringify({
        latMin: bounds.getSouth(), latMax: bounds.getNorth(),
        lngMin: bounds.getWest(), lngMax: bounds.getEast(),
        zoomMin, zoomMax
    });
    const estimate = await EstimateTiles(config);
    estimateEl.textContent = 'Estimate: ' + estimate;
}

// Update estimate on rectangle draw and zoom changes
document.querySelectorAll('#tile-zoom-min, #tile-zoom-max').forEach(el => {
    el.addEventListener('change', updateTileEstimate);
});

// ---- Tile preload ----
document.getElementById('btn-cancel-tiles').addEventListener('click', async () => {
    const r = await CancelPreload();
    document.getElementById('tile-progress-text').textContent = r === 'cancelled' ? 'Cancelled' : r;
    document.getElementById('tile-progress-bar').value = 0;
});

document.getElementById('btn-preload-tiles').addEventListener('click', async () => {
    const zoomMin = parseInt(document.getElementById('tile-zoom-min').value);
    const zoomMax = parseInt(document.getElementById('tile-zoom-max').value);
    const progressBar = document.getElementById('tile-progress-bar');
    const progressText = document.getElementById('tile-progress-text');

    if (isNaN(zoomMin) || isNaN(zoomMax)) {
        progressText.textContent = 'Please fill zoom range';
        return;
    }

    let bounds = null;
    if (drawnItems && drawnItems.getLayers().length > 0) {
        const layer = drawnItems.getLayers()[0];
        if (typeof layer.getBounds === 'function') {
            bounds = layer.getBounds();
        }
    }
    if (!bounds) {
        progressText.textContent = 'Please draw a rectangle on the map first';
        return;
    }

    progressBar.style.display = 'block';
    progressBar.value = 0;
    progressText.textContent = 'Starting preload...';

    const config = JSON.stringify({
        latMin: bounds.getSouth(), latMax: bounds.getNorth(),
        lngMin: bounds.getWest(), lngMax: bounds.getEast(),
        zoomMin, zoomMax
    });
    // Apply current rate limit settings dynamically
    await UpdateRateLimit(
        parseInt(document.getElementById('set-tile-concurrency').value) || 2
    );
    document.getElementById('btn-cancel-tiles').disabled = false;
    document.getElementById('btn-preload-tiles').disabled = true;
    PreloadTiles(config);
});

EventsOn('tileProgress', (msg) => {
    const progressBar = document.getElementById('tile-progress-bar');
    const progressText = document.getElementById('tile-progress-text');
    if (msg.startsWith('error') || msg.startsWith('cancelled') || msg.startsWith('done')) {
        progressText.textContent = msg;
        progressBar.value = msg.startsWith('done') ? 100 : 0;
        document.getElementById('btn-cancel-tiles').disabled = true;
        document.getElementById('btn-preload-tiles').disabled = false;
    } else {
        const parts = msg.split('/');
        if (parts.length === 2) {
            const loaded = parseInt(parts[0]);
            const total = parseInt(parts[1]);
            progressBar.value = total > 0 ? (loaded / total) * 100 : 0;
            progressText.textContent = msg + ' tiles';
        } else {
            progressText.textContent = msg;
        }
    }
});

// ---- Load initial settings + init ----
(async function init() {
    try {
        const raw = await GetSettings();
        const parsed = JSON.parse(raw);
        currentSettings = parsed;
        populateSimpleFields(parsed);
        setJson.value = JSON.stringify(parsed, null, 2);

        // Cache tile server URL for map
        window.tileServerURL = await GetTileServerURL();
    } catch (e) { /* ignore */ }

    setInterval(pollTelemetry, 500);
    setInterval(pollImage, 1000);
    setInterval(pollHistory, 2000);

    const appVersion = await GetVersion();
    document.title = "Goblin Decoder " + appVersion;
    const verEl = document.getElementById('app-version');
    if (verEl) verEl.textContent = appVersion;
    const aboutVerEl = document.getElementById('about-version');
    if (aboutVerEl) aboutVerEl.textContent = appVersion;
})();
