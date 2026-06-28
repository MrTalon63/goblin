import './style.css';

import {EventsOn, EventsEmit, BrowserOpenURL} from '../wailsjs/runtime/runtime';
import {TelemetrySnapshot, GetHistory, GetLatestImageInfo, GetSettings, SaveSettings, StartRecording, StopRecording, ClearCache, GetSessionPath, GetPayloadName, GetTileServerURL, PreloadTiles, EstimateTiles, UpdateRateLimit, CancelPreload, GetVersion, SelectDirectory, OpenSessionFolder, IsSessionActive} from '../wailsjs/go/main/App';

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
        document.getElementById('info-callsign').textContent = (d.core && d.core.callsign) || '--';
        document.getElementById('info-images').textContent = d.total || 0;

        const active = await IsSessionActive();
        const openBtn = document.getElementById('btn-open-rx-folder');
        if (openBtn) {
            if (active) {
                openBtn.disabled = false;
                openBtn.style.opacity = '1';
                openBtn.style.cursor = 'pointer';
            } else {
                openBtn.disabled = true;
                openBtn.style.opacity = '0.5';
                openBtn.style.cursor = 'not-allowed';
            }
        }
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
const setSondehubEnabled = document.getElementById('set-sondehub-enabled');
const setSondehubCallsign = document.getElementById('set-sondehub-callsign');
const setSondehubLat = document.getElementById('set-sondehub-lat');
const setSondehubLon = document.getElementById('set-sondehub-lon');
const setSondehubAlt = document.getElementById('set-sondehub-alt');
const setSondehubAntenna = document.getElementById('set-sondehub-antenna');
const setSondehubRadio = document.getElementById('set-sondehub-radio');

const setTelemetryEnabled = document.getElementById('set-telemetry-enabled');
const setTelemetryUrl = document.getElementById('set-telemetry-url');
const setTelemetryNickname = document.getElementById('set-telemetry-nickname');
const setTelemetryLat = document.getElementById('set-telemetry-lat');
const setTelemetryLon = document.getElementById('set-telemetry-lon');
const setTelemetryAlt = document.getElementById('set-telemetry-alt');
const setTelemetryAntenna = document.getElementById('set-telemetry-antenna');
const setTelemetryRadio = document.getElementById('set-telemetry-radio');

let currentSettings = {};

async function performSaveSettings() {
    setStatus.textContent = 'Saving...';
    const floatingStatus = document.getElementById('floating-save-status');
    if (floatingStatus) floatingStatus.textContent = 'Saving...';

    const json = setJson.value;
    let merged = JSON.parse(json);
    if (setSaveDir) merged.save_dir = setSaveDir.value;
    if (setMaxHistory) merged.max_history = parseInt(setMaxHistory.value) || 0;
    if (setTileDir) merged.tile_dir = setTileDir.value;
    if (setTileConcurrency) merged.tile_concurrency = parseInt(setTileConcurrency.value) || 2;
    if (setSondehubEnabled) merged.sondehub_enabled = setSondehubEnabled.checked;
    if (setSondehubCallsign) merged.sondehub_uploader_callsign = setSondehubCallsign.value.trim().toUpperCase();
    if (setSondehubLat) merged.sondehub_uploader_lat = parseFloat(setSondehubLat.value) || 0.0;
    if (setSondehubLon) merged.sondehub_uploader_lon = parseFloat(setSondehubLon.value) || 0.0;
    if (setSondehubAlt) merged.sondehub_uploader_alt = parseFloat(setSondehubAlt.value) || 0.0;
    if (setSondehubAntenna) merged.sondehub_uploader_antenna = setSondehubAntenna.value.trim();
    if (setSondehubRadio) merged.sondehub_uploader_radio = setSondehubRadio.value.trim();

    if (setTelemetryEnabled) merged.telemetry_server_enabled = setTelemetryEnabled.checked;
    if (setTelemetryUrl) merged.telemetry_server_url = setTelemetryUrl.value.trim();
    if (setTelemetryNickname) merged.telemetry_nickname = setTelemetryNickname.value.trim();
    if (setTelemetryLat) merged.telemetry_lat = parseFloat(setTelemetryLat.value) || 0.0;
    if (setTelemetryLon) merged.telemetry_lon = parseFloat(setTelemetryLon.value) || 0.0;
    if (setTelemetryAlt) merged.telemetry_alt = parseFloat(setTelemetryAlt.value) || 0.0;
    if (setTelemetryAntenna) merged.telemetry_antenna = setTelemetryAntenna.value.trim();
    if (setTelemetryRadio) merged.telemetry_radio = setTelemetryRadio.value.trim();

    const result = await SaveSettings(JSON.stringify(merged, null, 2));
    
    const statusText = result === 'ok' ? '✓ Saved' : '✗ ' + result;
    setStatus.textContent = statusText;
    if (floatingStatus) floatingStatus.textContent = statusText;

    if (result === 'ok') {
        currentSettings = merged;
        setJson.value = JSON.stringify(merged, null, 2);
        const floatingContainer = document.getElementById('floating-save-container');
        if (floatingContainer) floatingContainer.classList.add('hidden');
    }

    setTimeout(() => {
        setStatus.textContent = '';
        if (floatingStatus) floatingStatus.textContent = '';
    }, 3000);
}

document.getElementById('btn-save-settings').addEventListener('click', performSaveSettings);
const btnFloatingSave = document.getElementById('btn-floating-save');
if (btnFloatingSave) {
    btnFloatingSave.addEventListener('click', performSaveSettings);
}

function checkForUnsavedChanges() {
    if (!currentSettings || Object.keys(currentSettings).length === 0) return;

    let changed = false;

    if (setSaveDir && setSaveDir.value !== (currentSettings.save_dir || 'data')) changed = true;
    if (setMaxHistory && (parseInt(setMaxHistory.value) || 0) !== (currentSettings.max_history || 0)) changed = true;
    if (setTileDir && setTileDir.value !== (currentSettings.tile_dir || 'tiles')) changed = true;
    if (setTileConcurrency && (parseInt(setTileConcurrency.value) || 2) !== (currentSettings.tile_concurrency || 2)) changed = true;
    
    if (setSondehubEnabled && setSondehubEnabled.checked !== (currentSettings.sondehub_enabled || false)) changed = true;
    if (setSondehubCallsign && setSondehubCallsign.value.trim().toUpperCase() !== (currentSettings.sondehub_uploader_callsign || 'N0CALL').trim().toUpperCase()) changed = true;
    if (setSondehubLat && (parseFloat(setSondehubLat.value) || 0.0) !== (currentSettings.sondehub_uploader_lat || 0.0)) changed = true;
    if (setSondehubLon && (parseFloat(setSondehubLon.value) || 0.0) !== (currentSettings.sondehub_uploader_lon || 0.0)) changed = true;
    if (setSondehubAlt && (parseFloat(setSondehubAlt.value) || 0.0) !== (currentSettings.sondehub_uploader_alt || 0.0)) changed = true;
    if (setSondehubAntenna && setSondehubAntenna.value.trim() !== (currentSettings.sondehub_uploader_antenna || '')) changed = true;
    if (setSondehubRadio && setSondehubRadio.value.trim() !== (currentSettings.sondehub_uploader_radio || '')) changed = true;

    if (setTelemetryEnabled && setTelemetryEnabled.checked !== (currentSettings.telemetry_server_enabled || false)) changed = true;
    if (setTelemetryUrl && setTelemetryUrl.value.trim() !== (currentSettings.telemetry_server_url || '')) changed = true;
    if (setTelemetryNickname && setTelemetryNickname.value.trim() !== (currentSettings.telemetry_nickname || '')) changed = true;
    if (setTelemetryLat && (parseFloat(setTelemetryLat.value) || 0.0) !== (currentSettings.telemetry_lat || 0.0)) changed = true;
    if (setTelemetryLon && (parseFloat(setTelemetryLon.value) || 0.0) !== (currentSettings.telemetry_lon || 0.0)) changed = true;
    if (setTelemetryAlt && (parseFloat(setTelemetryAlt.value) || 0.0) !== (currentSettings.telemetry_alt || 0.0)) changed = true;
    if (setTelemetryAntenna && setTelemetryAntenna.value.trim() !== (currentSettings.telemetry_antenna || '')) changed = true;
    if (setTelemetryRadio && setTelemetryRadio.value.trim() !== (currentSettings.telemetry_radio || '')) changed = true;

    if (setJson) {
        try {
            const parsedJson = JSON.parse(setJson.value);
            if (JSON.stringify(parsedJson) !== JSON.stringify(currentSettings)) {
                changed = true;
            }
        } catch(e) {
            changed = true;
        }
    }

    const floatingContainer = document.getElementById('floating-save-container');
    if (floatingContainer) {
        const settingsTab = document.getElementById('tab-settings');
        if (changed && settingsTab && settingsTab.classList.contains('active')) {
            floatingContainer.classList.remove('hidden');
        } else {
            floatingContainer.classList.add('hidden');
        }
    }
}

// Add input/change event listeners to all settings fields to call checkForUnsavedChanges
setTimeout(() => {
    const inputsToWatch = [
        setSaveDir, setMaxHistory, setTileDir, setTileConcurrency,
        setSondehubEnabled, setSondehubCallsign, setSondehubLat,
        setSondehubLon, setSondehubAlt, setSondehubAntenna, setSondehubRadio,
        setTelemetryEnabled, setTelemetryUrl, setTelemetryNickname, setTelemetryLat,
        setTelemetryLon, setTelemetryAlt, setTelemetryAntenna, setTelemetryRadio,
        setJson
    ];
    inputsToWatch.forEach(input => {
        if (input) {
            input.addEventListener('input', checkForUnsavedChanges);
            input.addEventListener('change', checkForUnsavedChanges);
        }
    });
}, 500);

// Hide floating banner if we switch away from Settings tab
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        const floatingContainer = document.getElementById('floating-save-container');
        if (floatingContainer) {
            if (btn.dataset.tab !== 'settings') {
                floatingContainer.classList.add('hidden');
            } else {
                checkForUnsavedChanges();
            }
        }
    });
});

function populateSimpleFields(parsed) {
    if (setSaveDir) setSaveDir.value = parsed.save_dir || 'data';
    if (setMaxHistory) setMaxHistory.value = parsed.max_history || 50;
    if (setTileDir) setTileDir.value = parsed.tile_dir || 'tiles';
    if (setTileConcurrency) setTileConcurrency.value = parsed.tile_concurrency || 2;
    
    if (setSondehubEnabled) setSondehubEnabled.checked = parsed.sondehub_enabled || false;
    if (setSondehubCallsign) setSondehubCallsign.value = parsed.sondehub_uploader_callsign || 'N0CALL';
    if (setSondehubLat) setSondehubLat.value = parsed.sondehub_uploader_lat !== undefined ? parsed.sondehub_uploader_lat : 0.0;
    if (setSondehubLon) setSondehubLon.value = parsed.sondehub_uploader_lon !== undefined ? parsed.sondehub_uploader_lon : 0.0;
    if (setSondehubAlt) setSondehubAlt.value = parsed.sondehub_uploader_alt !== undefined ? parsed.sondehub_uploader_alt : 0.0;
    if (setSondehubAntenna) setSondehubAntenna.value = parsed.sondehub_uploader_antenna || '';
    if (setSondehubRadio) setSondehubRadio.value = parsed.sondehub_uploader_radio || '';

    if (setTelemetryEnabled) setTelemetryEnabled.checked = parsed.telemetry_server_enabled || false;
    if (setTelemetryUrl) setTelemetryUrl.value = parsed.telemetry_server_url || '';
    if (setTelemetryNickname) setTelemetryNickname.value = parsed.telemetry_nickname || '';
    if (setTelemetryLat) setTelemetryLat.value = parsed.telemetry_lat !== undefined ? parsed.telemetry_lat : 0.0;
    if (setTelemetryLon) setTelemetryLon.value = parsed.telemetry_lon !== undefined ? parsed.telemetry_lon : 0.0;
    if (setTelemetryAlt) setTelemetryAlt.value = parsed.telemetry_alt !== undefined ? parsed.telemetry_alt : 0.0;
    if (setTelemetryAntenna) setTelemetryAntenna.value = parsed.telemetry_antenna || '';
    if (setTelemetryRadio) setTelemetryRadio.value = parsed.telemetry_radio || '';
    
    // Call telemetry inputs disabled state check
    updateTelemetryInputsState();
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

document.getElementById('btn-open-rx-folder').addEventListener('click', () => {
    OpenSessionFolder();
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

// ---- Location Picker Map Popup ----
let popupMap = null;
let popupMarker = null;
let selectedLatLng = null;
let pickerTarget = 'sondehub'; // 'sondehub', 'telemetry', or 'prompt'

const mapPopupOverlay = document.getElementById('map-popup-overlay');
const btnPickLocationMap = document.getElementById('btn-pick-location-map');
const btnTelemetryPickLocationMap = document.getElementById('btn-telemetry-pick-location-map');
const btnPromptPickLocationMap = document.getElementById('btn-prompt-pick-location-map');
const btnPopupMapCancel = document.getElementById('btn-popup-map-cancel');
const btnPopupMapSelect = document.getElementById('btn-popup-map-select');
const mapPopupClose = document.getElementById('map-popup-close');
const elevationLoadingStatus = document.getElementById('elevation-loading-status');
const telemetryElevationStatus = document.getElementById('telemetry-elevation-status');

function openMapPicker(target) {
    if (!mapPopupOverlay) return;
    pickerTarget = target;
    mapPopupOverlay.classList.remove('hidden');

    let lat = 0.0;
    let lon = 0.0;
    if (target === 'sondehub') {
        lat = parseFloat(setSondehubLat.value) || 0.0;
        lon = parseFloat(setSondehubLon.value) || 0.0;
    } else if (target === 'telemetry') {
        lat = parseFloat(setTelemetryLat.value) || 0.0;
        lon = parseFloat(setTelemetryLon.value) || 0.0;
    } else if (target === 'prompt') {
        lat = parseFloat(document.getElementById('prompt-lat').value) || 0.0;
        lon = parseFloat(document.getElementById('prompt-lon').value) || 0.0;
    }
    if (lat === 0.0 && lon === 0.0) {
        lat = 50.0;
        lon = 20.0;
    }

    selectedLatLng = L.latLng(lat, lon);

    if (!popupMap) {
        popupMap = L.map('popup-map').setView([lat, lon], 10);
        const tileURL = window.tileServerURL || '';
        if (tileURL) {
            L.tileLayer(tileURL + '/tiles/{z}/{x}/{y}.png', {maxZoom: 19, attribution: 'OSM'}).addTo(popupMap);
        } else {
            L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {maxZoom: 19, attribution: 'OSM'}).addTo(popupMap);
        }

        popupMap.on('click', (e) => {
            selectedLatLng = e.latlng;
            if (!popupMarker) {
                popupMarker = L.marker(selectedLatLng).addTo(popupMap);
            } else {
                popupMarker.setLatLng(selectedLatLng);
            }
        });
    } else {
        popupMap.setView([lat, lon], 10);
    }

    setTimeout(() => {
        popupMap.invalidateSize();
        if (!popupMarker) {
            popupMarker = L.marker([lat, lon]).addTo(popupMap);
        } else {
            popupMarker.setLatLng([lat, lon]);
        }
    }, 100);
}

if (btnPickLocationMap) {
    btnPickLocationMap.addEventListener('click', () => openMapPicker('sondehub'));
}
if (btnTelemetryPickLocationMap) {
    btnTelemetryPickLocationMap.addEventListener('click', () => openMapPicker('telemetry'));
}
if (btnPromptPickLocationMap) {
    btnPromptPickLocationMap.addEventListener('click', () => openMapPicker('prompt'));
}

const hideMapPopup = () => {
    if (mapPopupOverlay) mapPopupOverlay.classList.add('hidden');
};

if (btnPopupMapCancel) btnPopupMapCancel.addEventListener('click', hideMapPopup);
if (mapPopupClose) mapPopupClose.addEventListener('click', hideMapPopup);

if (btnPopupMapSelect) {
    btnPopupMapSelect.addEventListener('click', async () => {
        hideMapPopup();
        if (!selectedLatLng) return;

        let statusEl = elevationLoadingStatus;
        let latInput = setSondehubLat;
        let lonInput = setSondehubLon;
        let altInput = setSondehubAlt;

        if (pickerTarget === 'telemetry') {
            statusEl = telemetryElevationStatus;
            latInput = setTelemetryLat;
            lonInput = setTelemetryLon;
            altInput = setTelemetryAlt;
        } else if (pickerTarget === 'prompt') {
            statusEl = document.getElementById('prompt-elevation-status');
            latInput = document.getElementById('prompt-lat');
            lonInput = document.getElementById('prompt-lon');
            altInput = document.getElementById('prompt-alt');
        }

        if (latInput) latInput.value = selectedLatLng.lat.toFixed(6);
        if (lonInput) lonInput.value = selectedLatLng.lng.toFixed(6);

        if (statusEl) {
            statusEl.textContent = 'Fetching altitude...';
            statusEl.style.color = '#aaa';
        }

        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 3000);

        try {
            const url = `https://api.open-meteo.com/v1/elevation?latitude=${selectedLatLng.lat}&longitude=${selectedLatLng.lng}`;
            const res = await fetch(url, { signal: controller.signal });
            clearTimeout(timeoutId);
            const data = await res.json();
            if (data && data.elevation && data.elevation.length > 0) {
                const elev = data.elevation[0];
                if (altInput) {
                    altInput.value = elev.toFixed(1);
                }
                if (statusEl) {
                    statusEl.textContent = `✓ Altitude: ${elev.toFixed(1)}m`;
                    statusEl.style.color = '#0f0';
                }
                // Trigger unsaved setting check manually
                checkForUnsavedChanges();
            } else {
                throw new Error("Empty elevation data");
            }
        } catch (e) {
            clearTimeout(timeoutId);
            console.error("Elevation API lookup failed:", e);
            if (statusEl) {
                statusEl.textContent = '✗ Fetch failed (timeout or error)';
                statusEl.style.color = '#f00';
            }
        }
    });
}

const telemetryPromptUrlLink = document.getElementById('telemetry-prompt-url-link');
if (telemetryPromptUrlLink) {
    telemetryPromptUrlLink.addEventListener('click', (e) => {
        e.preventDefault();
        BrowserOpenURL('https://goblin.mrtalon.eu');
    });
}

const aboutGithub = document.getElementById('about-github');
if (aboutGithub) {
    aboutGithub.addEventListener('click', (e) => {
        e.preventDefault();
        BrowserOpenURL('https://github.com/MrTalon63/goblin/');
    });
}

function updateTelemetryInputsState() {
    const isTelemEnabled = setTelemetryEnabled ? setTelemetryEnabled.checked : false;
    const telemInputs = [
        setTelemetryUrl, setTelemetryNickname, setTelemetryLat,
        setTelemetryLon, setTelemetryAlt, document.getElementById('btn-telemetry-pick-location-map'),
        setTelemetryAntenna, setTelemetryRadio
    ];
    telemInputs.forEach(input => {
        if (input) {
            input.disabled = !isTelemEnabled;
            input.style.opacity = isTelemEnabled ? '1' : '0.5';
            input.style.cursor = isTelemEnabled ? 'auto' : 'not-allowed';
        }
    });

    const promptTelemEnabled = document.getElementById('prompt-telemetry-enabled');
    const isPromptEnabled = promptTelemEnabled ? promptTelemEnabled.checked : false;
    const promptInputs = [
        document.getElementById('prompt-nickname'), document.getElementById('prompt-lat'),
        document.getElementById('prompt-lon'), document.getElementById('prompt-alt'),
        document.getElementById('btn-prompt-pick-location-map'), document.getElementById('prompt-antenna'),
        document.getElementById('prompt-radio')
    ];
    promptInputs.forEach(input => {
        if (input) {
            input.disabled = !isPromptEnabled;
            input.style.opacity = isPromptEnabled ? '1' : '0.5';
            input.style.cursor = isPromptEnabled ? 'auto' : 'not-allowed';
        }
    });
}

if (setTelemetryEnabled) {
    setTelemetryEnabled.addEventListener('change', updateTelemetryInputsState);
}

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

        // First launch telemetry opt-in prompt
        if (!parsed.telemetry_prompted) {
            const promptOverlay = document.getElementById('telemetry-prompt-overlay');
            if (promptOverlay) {
                promptOverlay.classList.remove('hidden');

                // Bind toggle listener inside modal
                const promptTelemEnabled = document.getElementById('prompt-telemetry-enabled');
                if (promptTelemEnabled) {
                    promptTelemEnabled.addEventListener('change', updateTelemetryInputsState);
                }

                // Leave inputs empty by default (rely on placeholder texts)
                const promptNickname = document.getElementById('prompt-nickname');
                const promptLat = document.getElementById('prompt-lat');
                const promptLon = document.getElementById('prompt-lon');
                const promptAlt = document.getElementById('prompt-alt');
                const promptAntenna = document.getElementById('prompt-antenna');
                const promptRadio = document.getElementById('prompt-radio');

                if (promptNickname) promptNickname.value = '';
                if (promptLat) promptLat.value = '';
                if (promptLon) promptLon.value = '';
                if (promptAlt) promptAlt.value = '';
                if (promptAntenna) promptAntenna.value = '';
                if (promptRadio) promptRadio.value = '';

                // Initialize prompt disabled states
                updateTelemetryInputsState();

                const btnPromptSave = document.getElementById('btn-prompt-save');
                if (btnPromptSave) {
                    btnPromptSave.addEventListener('click', async () => {
                        const contributeChecked = promptTelemEnabled ? promptTelemEnabled.checked : false;
                        
                        parsed.telemetry_server_enabled = contributeChecked;
                        parsed.telemetry_prompted = true;
                        
                        if (contributeChecked) {
                            parsed.telemetry_nickname = promptNickname ? promptNickname.value.trim() : '';
                            parsed.telemetry_lat = (promptLat && promptLat.value !== '') ? parseFloat(promptLat.value) : 0.0;
                            parsed.telemetry_lon = (promptLon && promptLon.value !== '') ? parseFloat(promptLon.value) : 0.0;
                            parsed.telemetry_alt = (promptAlt && promptAlt.value !== '') ? parseFloat(promptAlt.value) : 0.0;
                            parsed.telemetry_antenna = promptAntenna ? promptAntenna.value.trim() : '';
                            parsed.telemetry_radio = promptRadio ? promptRadio.value.trim() : '';
                        }

                        // Save updated configuration in Go app
                        await SaveSettings(JSON.stringify(parsed, null, 2));

                        // Reload settings in memory and Simple UI
                        currentSettings = parsed;
                        populateSimpleFields(parsed);
                        setJson.value = JSON.stringify(parsed, null, 2);

                        // Hide prompt overlay
                        promptOverlay.classList.add('hidden');
                    });
                }
            }
        }
    } catch (e) { /* ignore */ }

    setInterval(pollTelemetry, 500);
    setInterval(pollImage, 1000);
    setInterval(pollHistory, 2000);

    const appVersion = await GetVersion();
    document.title = "Goblin Decoder " + appVersion;
    const aboutVerEl = document.getElementById('about-version');
    if (aboutVerEl) aboutVerEl.textContent = appVersion;
})();
