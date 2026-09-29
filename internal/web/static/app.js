const state = {
  summary: null,
  files: [],
  activeFile: null,
  currentPage: 0,
  pageSize: 100,
  totalFiltered: 0,
  selectedEntry: null,
  activeTab: 'explorer',
  inContextMode: false,
  contextTargetLine: null,
  contextRadius: 100,
  savedContextState: null,
  filters: {
    q: '',
    level: '',
    category: '',
    from: '',
    to: '',
    hide_noise: true,
    only_errors: false,
    all_rows: false,
    pump: '',
    dispatch: ''
  }
};

let debounceTimer = null;

function formatToInputDateTime(str) {
  if (!str) return '';
  const parts = str.split(' ');
  if (parts.length < 2) return '';
  const datePart = parts[0];
  const timePart = parts[1].split('.')[0];
  return `${datePart}T${timePart}`;
}

// Initialize on DOM Ready
document.addEventListener('DOMContentLoaded', () => {
  initTabs();
  initFilters();
  initModal();
  initFilesManager();
  initAIDebugger();
  fetchFiles();
  fetchSummary();
  fetchLogs();
});

// Tab Navigation
function initTabs() {
  const tabButtons = document.querySelectorAll('.tab-btn');
  tabButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      const tabId = btn.dataset.tab;
      switchTab(tabId);
    });
  });
}

function switchTab(tabId) {
  state.activeTab = tabId;
  document.querySelectorAll('.tab-btn').forEach(b => {
    b.classList.toggle('active', b.dataset.tab === tabId);
  });
  document.querySelectorAll('.tab-pane').forEach(p => {
    p.classList.toggle('active', p.id === `pane-${tabId}`);
  });
}

// ==========================================
// File Registry & Upload Management
// ==========================================
function initFilesManager() {
  const filesModal = document.getElementById('files-modal');
  const btnOpenModal = document.getElementById('btn-open-files-modal');
  const btnQuickUpload = document.getElementById('btn-quick-upload');
  const btnCloseModal = document.getElementById('btn-close-files-modal');
  const btnCloseAction = document.getElementById('btn-close-files-action');
  
  const dropzone = document.getElementById('upload-dropzone');
  const fileInput = document.getElementById('file-input-element');

  const openModal = () => {
    fetchFiles();
    filesModal.classList.add('active');
  };
  const closeModal = () => filesModal.classList.remove('active');

  btnOpenModal.addEventListener('click', openModal);
  btnQuickUpload.addEventListener('click', openModal);
  btnCloseModal.addEventListener('click', closeModal);
  btnCloseAction.addEventListener('click', closeModal);

  filesModal.addEventListener('click', (e) => {
    if (e.target === filesModal) closeModal();
  });

  // Dropzone click -> trigger file input
  dropzone.addEventListener('click', (e) => {
    if (e.target.closest('#upload-progress-wrapper')) return;
    fileInput.click();
  });

  fileInput.addEventListener('change', () => {
    if (fileInput.files && fileInput.files.length > 0) {
      uploadFile(fileInput.files[0]);
    }
  });

  // Drag & drop handlers
  ['dragenter', 'dragover'].forEach(eventName => {
    dropzone.addEventListener(eventName, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropzone.classList.add('dragover');
    }, false);
  });

  ['dragleave', 'drop'].forEach(eventName => {
    dropzone.addEventListener(eventName, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropzone.classList.remove('dragover');
    }, false);
  });

  dropzone.addEventListener('drop', (e) => {
    const dt = e.dataTransfer;
    const files = dt.files;
    if (files && files.length > 0) {
      uploadFile(files[0]);
    }
  });

  // Local Path Registration Handler (for 7GB+ files already on disk)
  const btnRegisterPath = document.getElementById('btn-register-local-path');
  const inputLocalPath = document.getElementById('input-local-path');

  btnRegisterPath.addEventListener('click', async () => {
    const p = inputLocalPath.value.trim();
    if (!p) {
      alert('Por favor introduce la ruta completa del archivo en disco.');
      return;
    }
    btnRegisterPath.disabled = true;
    btnRegisterPath.textContent = 'Indexando...';
    try {
      const res = await fetch('/api/register_path', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: p })
      });
      if (!res.ok) {
        const errData = await res.json();
        throw new Error(errData.error || 'Error al registrar archivo');
      }
      inputLocalPath.value = '';
      await fetchFiles();
      await fetchSummary();
      await fetchLogs();
      document.getElementById('files-modal').classList.remove('active');
    } catch (err) {
      alert(`Error: ${err.message}`);
    } finally {
      btnRegisterPath.disabled = false;
      btnRegisterPath.textContent = 'Registrar & Indexar';
    }
  });
}

// Fetch list of registered files
async function fetchFiles() {
  try {
    const res = await fetch('/api/files');
    if (!res.ok) throw new Error('Error al listar archivos');
    const files = await res.json();
    state.files = files;
    renderFilesRegistry(files);

    const active = files.find(f => f.is_active);
    if (active) {
      state.activeFile = active;
      document.getElementById('active-file-label').textContent = active.name;
      document.getElementById('header-filesize').textContent = `${active.size_mb.toFixed(1)} MB`;
    }
  } catch (err) {
    console.error('Error fetching files:', err);
  }
}

function renderFilesRegistry(files) {
  const tbody = document.getElementById('files-registry-tbody');
  tbody.innerHTML = '';

  if (!files || files.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="empty-state">No hay archivos registrados.</td></tr>`;
    return;
  }

  files.forEach(f => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>
        <strong>${f.name}</strong>
        ${f.is_uploaded ? '<span class="badge-count" style="margin-left: 0.35rem; font-size:0.65rem;">subido</span>' : '<span class="badge-count" style="margin-left: 0.35rem; font-size:0.65rem;">raíz</span>'}
      </td>
      <td>${f.size_mb.toFixed(1)} MB</td>
      <td><span class="dim-text">${f.mod_time}</span></td>
      <td>${f.parsed ? f.total_lines.toLocaleString() : '<span class="dim-text">Al abrir</span>'}</td>
      <td>${f.parsed ? `<span class="error-text font-bold">${f.error_count.toLocaleString()}</span>` : '--'}</td>
      <td>
        ${f.is_active 
          ? '<span class="status-pill active"><span class="dot pulse-emerald"></span> Activo</span>' 
          : '<span class="status-pill inactive">Disponible</span>'}
      </td>
      <td>
        ${f.is_active 
          ? '<span class="dim-text" style="font-size:0.75rem;">En pantalla</span>' 
          : `<button class="btn-select-file" onclick="selectLogFile('${f.name}')">Cargar</button>`}
        ${f.is_uploaded && !f.is_active 
          ? `<button class="btn-delete-file" title="Eliminar archivo" onclick="deleteLogFile('${f.name}')">&times;</button>` 
          : ''}
      </td>
    `;
    tbody.appendChild(tr);
  });
}

// Select/Switch active file
window.selectLogFile = async function(fileName) {
  try {
    const res = await fetch(`/api/select?file=${encodeURIComponent(fileName)}`, {
      method: 'POST'
    });
    if (!res.ok) throw new Error('Error al seleccionar archivo');
    const data = await res.json();
    
    // Update local state
    state.activeFile = data.file;
    state.summary = data.summary;
    state.currentPage = 0;

    renderSummary(data.summary);
    await fetchFiles();
    await fetchLogs();

    document.getElementById('files-modal').classList.remove('active');
  } catch (err) {
    alert(`Error: ${err.message}`);
  }
};

// Upload log file
function uploadFile(file) {
  if (!file) return;

  const validExts = ['.log', '.txt'];
  const hasValidExt = validExts.some(ext => file.name.toLowerCase().endsWith(ext));
  if (!hasValidExt) {
    alert('Por favor selecciona un archivo con extensión .log o .txt');
    return;
  }

  const progressWrapper = document.getElementById('upload-progress-wrapper');
  const progressBar = document.getElementById('upload-progress-bar');
  const statusText = document.getElementById('upload-status-text');

  progressWrapper.classList.add('active');
  progressBar.style.width = '10%';
  statusText.textContent = `Subiendo '${file.name}' (${(file.size / (1024*1024)).toFixed(1)} MB)...`;

  const formData = new FormData();
  formData.append('log_file', file);

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/upload', true);

  xhr.upload.onprogress = (e) => {
    if (e.lengthComputable) {
      const percent = Math.round((e.loaded / e.total) * 85);
      progressBar.style.width = `${percent}%`;
    }
  };

  xhr.onload = async () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      progressBar.style.width = '100%';
      statusText.textContent = '¡Análisis completado exitosamente!';

      setTimeout(async () => {
        progressWrapper.classList.remove('active');
        progressBar.style.width = '0%';
        document.getElementById('file-input-element').value = '';
        
        await fetchFiles();
        await fetchSummary();
        await fetchLogs();
        document.getElementById('files-modal').classList.remove('active');
      }, 500);
    } else {
      progressWrapper.classList.remove('active');
      alert(`Error al procesar archivo: ${xhr.responseText}`);
    }
  };

  xhr.onerror = () => {
    progressWrapper.classList.remove('active');
    alert('Error de red durante la subida del archivo.');
  };

  xhr.send(formData);
}

// Delete uploaded log file
window.deleteLogFile = async function(fileName) {
  if (!confirm(`¿Eliminar el archivo '${fileName}'?`)) return;
  try {
    const res = await fetch(`/api/files?file=${encodeURIComponent(fileName)}`, {
      method: 'DELETE'
    });
    if (!res.ok) throw new Error('Error al eliminar archivo');
    await fetchFiles();
  } catch (err) {
    alert(`Error: ${err.message}`);
  }
};

// ==========================================
// Fetch and Populate Executive Summary
// ==========================================
async function fetchSummary() {
  try {
    const res = await fetch('/api/summary');
    if (!res.ok) throw new Error('Error al cargar resumen');
    const data = await res.json();
    state.summary = data;
    renderSummary(data);
  } catch (err) {
    console.error('Error fetching summary:', err);
  }
}

function renderSummary(data) {
  if (!data) return;

  // Header
  const fname = data.file_path ? data.file_path.split('/').pop() : 'Sin archivo';
  document.getElementById('active-file-label').textContent = fname;
  document.getElementById('header-filesize').textContent = `${data.file_size_mb.toFixed(1)} MB`;
  document.getElementById('header-parsetime').textContent = `Procesado en ${data.parse_time_ms} ms`;
  document.getElementById('log-subtitle').textContent = `Periodo: ${data.start_time} - ${data.end_time} (${data.duration})`;

  // KPIs
  document.getElementById('kpi-total-lines').textContent = data.total_lines.toLocaleString();
  document.getElementById('kpi-duration').textContent = `Duración: ${data.duration}`;
  document.getElementById('kpi-real-errors').textContent = data.real_errors_count.toLocaleString();
  document.getElementById('kpi-declared-errors').textContent = (data.level_counts['ERROR'] || 0).toLocaleString();
  document.getElementById('kpi-stuck-payments').textContent = (data.stuck_payments ? data.stuck_payments.length : 0);
  document.getElementById('kpi-hw-alerts').textContent = (data.hardware_alerts ? data.hardware_alerts.length : 0);
  document.getElementById('kpi-noise-percent').textContent = `${data.noise_percent.toFixed(1)}%`;
  document.getElementById('kpi-noise-count').textContent = data.noise_count.toLocaleString();

  // Badge counters
  document.getElementById('badge-payments-count').textContent = data.stuck_payments ? data.stuck_payments.length : 0;
  document.getElementById('badge-hardware-count').textContent = data.hardware_alerts ? data.hardware_alerts.length : 0;
  document.getElementById('badge-exceptions-count').textContent = data.exceptions ? data.exceptions.length : 0;

  // Configure Date/Time bounds based on first and last log
  const inputFrom = document.getElementById('filter-time-from');
  const inputTo = document.getElementById('filter-time-to');
  if (inputFrom && inputTo && data.start_time && data.end_time) {
    const minVal = formatToInputDateTime(data.start_time);
    const maxVal = formatToInputDateTime(data.end_time);
    inputFrom.min = minVal;
    inputFrom.max = maxVal;
    inputTo.min = minVal;
    inputTo.max = maxVal;

    // Set initial values if not set
    if (!state.filters.from) {
      inputFrom.value = minVal;
      state.filters.from = minVal;
    }
    if (!state.filters.to) {
      inputTo.value = maxVal;
      state.filters.to = maxVal;
    }
  }

  // Render Sub-Views
  renderPaymentsTable(data.stuck_payments || []);
  renderHardwarePane(data.hardware_alerts || []);
  renderExceptionsPane(data.exceptions || []);
  renderNoisePane(data.noise_breakdown || []);
}

// 1. Stuck Payments Tab
function renderPaymentsTable(payments) {
  const tbody = document.getElementById('payments-tbody');
  tbody.innerHTML = '';

  if (payments.length === 0) {
    tbody.innerHTML = `<tr><td colspan="7" class="empty-state">No hay pagos atorados reportados.</td></tr>`;
    return;
  }

  payments.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td><span class="dispatch-tag">${p.dispatch_id}</span></td>
      <td><span class="amount-tag">${p.amount || '--'}</span></td>
      <td><span class="retry-badge">${p.retries} reintentos</span></td>
      <td>${p.first_seen}</td>
      <td>${p.last_seen}</td>
      <td><span class="error-text">${p.last_error}</span></td>
      <td>
        <button class="btn-trace-sm" onclick="traceEntity('${p.dispatch_id}')" title="Filtrar eventos de este despacho">
          Rastrear
        </button>
        <button class="btn-trace-sm" onclick="jumpToLinearContext(${p.last_line || p.first_line})" title="Ver este pago en el flujo continuo del log">
          📍 Contexto
        </button>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

// 2. Hardware Diagnostics Tab
function renderHardwarePane(alerts) {
  const cardsContainer = document.getElementById('hw-cards-container');
  const tbody = document.getElementById('hw-alerts-tbody');
  cardsContainer.innerHTML = '';
  tbody.innerHTML = '';

  const devices = [
    { name: 'CCTALK [Billetero / Monedero]', id: 'CCTALK[3]', hasError: true, desc: 'Falla: Dispositivo no iniciado (Código: 30-00-00-3-00FF)' },
    { name: 'LCDM1000 [Dispensador de Efectivo]', id: 'LCDM1000[5]', hasError: true, desc: 'Falla: No fue posible abrir el puerto serial/comunicación' },
    { name: 'MEI-SCx [Aceptador SC Adv]', id: 'MEI-SCx[1]', hasError: false, desc: 'Operativo: Alarma apagada con éxito, comandos ENTER/EXIT' }
  ];

  devices.forEach(d => {
    const card = document.createElement('div');
    card.className = `hw-card ${d.hasError ? 'has-error' : ''}`;
    card.innerHTML = `
      <div class="hw-header">
        <div class="hw-name">${d.name}</div>
        <span class="hw-status-badge ${d.hasError ? 'error' : 'ok'}">
          ${d.hasError ? 'FALLA REPORTADA' : 'OPERATIVO'}
        </span>
      </div>
      <div class="hw-details">${d.desc}</div>
    `;
    cardsContainer.appendChild(card);
  });

  if (alerts.length === 0) {
    tbody.innerHTML = `<tr><td colspan="6" class="empty-state">No se registraron alertas de hardware.</td></tr>`;
    return;
  }

  alerts.forEach(a => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${a.timestamp}</td>
      <td><span class="dispatch-tag">L${a.line_number}</span></td>
      <td><strong>${a.device || 'Cajero'}</strong></td>
      <td><code>${a.error_code || '--'}</code></td>
      <td>${a.description}</td>
      <td>
        <button class="btn-trace-sm" onclick="jumpToLinearContext(${a.line_number})" title="Ver este error en su contexto continuo">
          📍 Ver en Log (L${a.line_number})
        </button>
      </td>
    `;
    tbody.appendChild(tr);
  });
}

// 3. Exceptions Tab
function renderExceptionsPane(exceptions) {
  const list = document.getElementById('exceptions-list');
  list.innerHTML = '';

  if (exceptions.length === 0) {
    list.innerHTML = `<div class="empty-state">No se encontraron excepciones.</div>`;
    return;
  }

  exceptions.forEach(ex => {
    const card = document.createElement('div');
    card.className = 'exception-card';
    card.innerHTML = `
      <div class="ex-top">
        <div class="ex-type">${ex.type}</div>
        <div class="ex-count-badge">${ex.count.toLocaleString()} ocurrencias</div>
      </div>
      ${ex.param_name ? `<div class="ex-param">Parámetro afectado: <strong>${ex.param_name}</strong></div>` : ''}
      <div class="ex-sample">${ex.sample_msg}</div>
    `;
    list.appendChild(card);
  });
}

// 4. Noise Breakdown Tab
function renderNoisePane(noiseList) {
  const container = document.getElementById('noise-cards-container');
  container.innerHTML = '';

  if (!noiseList || noiseList.length === 0) {
    container.innerHTML = '<div class="empty-state">No se detectó ruido relevante en este archivo.</div>';
    return;
  }

  noiseList.forEach(item => {
    const card = document.createElement('div');
    card.className = 'noise-card';
    card.innerHTML = `
      <div class="noise-pattern-title">${item.pattern}</div>
      <div class="noise-pattern-count">${item.count.toLocaleString()}</div>
      <div class="noise-bar-container">
        <div class="noise-bar-fill" style="width: ${Math.min(100, item.percent * 1.8)}%"></div>
      </div>
      <div class="kpi-meta">${item.percent.toFixed(1)}% del log total</div>
    `;
    container.appendChild(card);
  });
}

// ==========================================
// Filters & Search Handling
// ==========================================
function initFilters() {
  const inputQuery = document.getElementById('filter-query');
  const btnClear = document.getElementById('btn-clear-query');
  const checkHideNoise = document.getElementById('filter-hide-noise');
  const checkOnlyErrors = document.getElementById('filter-only-errors');
  const checkAllRows = document.getElementById('filter-all-rows');
  const selectCategory = document.getElementById('filter-category');
  const selectLevel = document.getElementById('filter-level');
  const btnPrev = document.getElementById('btn-prev-page');
  const btnNext = document.getElementById('btn-next-page');

  const inputTimeFrom = document.getElementById('filter-time-from');
  const inputTimeTo = document.getElementById('filter-time-to');
  const btnResetTime = document.getElementById('btn-reset-time-range');

  const btnContextMoreBefore = document.getElementById('btn-context-more-before');
  const btnContextMoreAfter = document.getElementById('btn-context-more-after');
  const btnContextReturn = document.getElementById('btn-context-return');

  if (inputTimeFrom) {
    inputTimeFrom.addEventListener('change', (e) => {
      state.filters.from = e.target.value;
      state.currentPage = 0;
      fetchLogs();
    });
  }

  if (inputTimeTo) {
    inputTimeTo.addEventListener('change', (e) => {
      state.filters.to = e.target.value;
      state.currentPage = 0;
      fetchLogs();
    });
  }

  if (btnResetTime) {
    btnResetTime.addEventListener('click', () => {
      if (state.summary && state.summary.start_time && state.summary.end_time) {
        const minVal = formatToInputDateTime(state.summary.start_time);
        const maxVal = formatToInputDateTime(state.summary.end_time);
        if (inputTimeFrom) inputTimeFrom.value = minVal;
        if (inputTimeTo) inputTimeTo.value = maxVal;
        state.filters.from = minVal;
        state.filters.to = maxVal;
        state.currentPage = 0;
        fetchLogs();
      }
    });
  }

  if (checkAllRows) {
    checkAllRows.addEventListener('change', (e) => {
      state.filters.all_rows = e.target.checked;
      state.currentPage = 0;
      fetchLogs();
    });
  }

  if (btnContextMoreBefore) {
    btnContextMoreBefore.addEventListener('click', () => {
      state.contextRadius += 100;
      if (state.contextTargetLine) {
        fetchContextLogs(state.contextTargetLine, state.contextRadius);
      }
    });
  }

  if (btnContextMoreAfter) {
    btnContextMoreAfter.addEventListener('click', () => {
      state.contextRadius += 100;
      if (state.contextTargetLine) {
        fetchContextLogs(state.contextTargetLine, state.contextRadius);
      }
    });
  }

  if (btnContextReturn) {
    btnContextReturn.addEventListener('click', () => {
      exitLinearContext();
    });
  }

  inputQuery.addEventListener('input', (e) => {
    const val = e.target.value;
    btnClear.classList.toggle('visible', val.length > 0);
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      state.filters.q = val;
      state.currentPage = 0;
      fetchLogs();
    }, 250);
  });

  btnClear.addEventListener('click', () => {
    inputQuery.value = '';
    btnClear.classList.remove('visible');
    state.filters.q = '';
    state.currentPage = 0;
    fetchLogs();
  });

  checkHideNoise.addEventListener('change', (e) => {
    state.filters.hide_noise = e.target.checked;
    state.currentPage = 0;
    fetchLogs();
  });

  checkOnlyErrors.addEventListener('change', (e) => {
    state.filters.only_errors = e.target.checked;
    state.currentPage = 0;
    fetchLogs();
  });

  selectCategory.addEventListener('change', (e) => {
    state.filters.category = e.target.value;
    state.currentPage = 0;
    fetchLogs();
  });

  selectLevel.addEventListener('change', (e) => {
    state.filters.level = e.target.value;
    state.currentPage = 0;
    fetchLogs();
  });

  btnPrev.addEventListener('click', () => {
    if (state.currentPage > 0) {
      state.currentPage--;
      fetchLogs();
    }
  });

  btnNext.addEventListener('click', () => {
    const maxPages = Math.ceil(state.totalFiltered / state.pageSize);
    if (state.currentPage + 1 < maxPages) {
      state.currentPage++;
      fetchLogs();
    }
  });
}

// Fetch Log Entries with Query Params
async function fetchLogs() {
  if (state.inContextMode) {
    return;
  }

  const container = document.getElementById('log-stream-list');
  container.innerHTML = '<div class="empty-state">Consultando telemetría...</div>';

  const params = new URLSearchParams();
  if (state.filters.q) params.set('q', state.filters.q);
  if (state.filters.level) params.set('level', state.filters.level);
  if (state.filters.category) params.set('category', state.filters.category);
  if (state.filters.hide_noise) params.set('hide_noise', 'true');
  if (state.filters.only_errors) params.set('only_errors', 'true');
  if (state.filters.pump) params.set('pump', state.filters.pump);
  if (state.filters.dispatch) params.set('dispatch', state.filters.dispatch);
  if (state.filters.from) params.set('from', state.filters.from);
  if (state.filters.to) params.set('to', state.filters.to);

  if (state.filters.all_rows) {
    params.set('all', 'true');
    params.set('limit', 10000);
    params.set('offset', 0);
  } else {
    params.set('limit', state.pageSize);
    params.set('offset', state.currentPage * state.pageSize);
  }

  try {
    const res = await fetch(`/api/logs?${params.toString()}`);
    if (!res.ok) throw new Error('Error al filtrar logs');
    const data = await res.json();
    state.totalFiltered = data.total_filtered;
    renderLogEntries(data.entries || []);
    updatePagination(data.total_filtered, false, (data.entries || []).length);
  } catch (err) {
    console.error('Error fetching logs:', err);
    container.innerHTML = `<div class="empty-state error-text">Error al cargar registros: ${err.message}</div>`;
  }
}

// Linear Context Operations
async function jumpToLinearContext(lineNumber) {
  if (!lineNumber) return;

  // Save current state if not already in context mode
  if (!state.inContextMode) {
    state.savedContextState = {
      filters: JSON.parse(JSON.stringify(state.filters)),
      currentPage: state.currentPage,
      activeTab: state.activeTab,
      scrollY: window.scrollY
    };
  }

  state.inContextMode = true;
  state.contextTargetLine = lineNumber;
  state.contextRadius = 100;

  // Ensure explorer tab is active
  if (state.activeTab !== 'explorer') {
    switchTab('explorer');
  }

  // Show banner
  const banner = document.getElementById('linear-context-banner');
  const lineLabel = document.getElementById('context-line-label');
  if (banner && lineLabel) {
    lineLabel.textContent = `Línea #${lineNumber.toLocaleString()}`;
    banner.style.display = 'flex';
  }

  await fetchContextLogs(lineNumber, state.contextRadius);
}

async function fetchContextLogs(lineNumber, radius = 100) {
  const container = document.getElementById('log-stream-list');
  container.innerHTML = `<div class="empty-state">Cargando registros contiguos alrededor de L${lineNumber}...</div>`;

  try {
    const res = await fetch(`/api/context?line=${lineNumber}&radius=${radius}`);
    if (!res.ok) throw new Error('Error al consultar contexto');
    const data = await res.json();
    renderLogEntries(data.entries || [], lineNumber);
    updatePagination(data.total_filtered, true, (data.entries || []).length);
  } catch (err) {
    console.error('Error fetching context:', err);
    container.innerHTML = `<div class="empty-state error-text">Error al cargar contexto: ${err.message}</div>`;
  }
}

function exitLinearContext() {
  state.inContextMode = false;
  state.contextTargetLine = null;

  const banner = document.getElementById('linear-context-banner');
  if (banner) banner.style.display = 'none';

  if (state.savedContextState) {
    // Restore filters
    state.filters = JSON.parse(JSON.stringify(state.savedContextState.filters));
    state.currentPage = state.savedContextState.currentPage;

    // Restore DOM inputs
    const inputQuery = document.getElementById('filter-query');
    const selectLevel = document.getElementById('filter-level');
    const selectCategory = document.getElementById('filter-category');
    const checkHideNoise = document.getElementById('filter-hide-noise');
    const checkOnlyErrors = document.getElementById('filter-only-errors');
    const checkAllRows = document.getElementById('filter-all-rows');
    const inputTimeFrom = document.getElementById('filter-time-from');
    const inputTimeTo = document.getElementById('filter-time-to');
    const btnClear = document.getElementById('btn-clear-query');

    if (inputQuery) {
      inputQuery.value = state.filters.q || '';
      if (btnClear) btnClear.classList.toggle('visible', (state.filters.q || '').length > 0);
    }
    if (selectLevel) selectLevel.value = state.filters.level || '';
    if (selectCategory) selectCategory.value = state.filters.category || '';
    if (checkHideNoise) checkHideNoise.checked = !!state.filters.hide_noise;
    if (checkOnlyErrors) checkOnlyErrors.checked = !!state.filters.only_errors;
    if (checkAllRows) checkAllRows.checked = !!state.filters.all_rows;
    if (inputTimeFrom) inputTimeFrom.value = state.filters.from || '';
    if (inputTimeTo) inputTimeTo.value = state.filters.to || '';

    // Restore previous tab if not explorer
    const prevTab = state.savedContextState.activeTab;
    if (prevTab && prevTab !== 'explorer') {
      switchTab(prevTab);
    } else {
      fetchLogs();
    }
  } else {
    fetchLogs();
  }
}

function renderLogEntries(entries, targetLine = null) {
  const container = document.getElementById('log-stream-list');
  container.innerHTML = '';

  if (entries.length === 0) {
    container.innerHTML = '<div class="empty-state">No se encontraron eventos con los filtros actuales.</div>';
    return;
  }

  entries.forEach(e => {
    const row = document.createElement('div');
    let rowClass = 'log-row';
    if (targetLine && e.line_number === targetLine) rowClass += ' is-target-context';
    else if (e.is_real_error || e.level === 'ERROR') rowClass += ' is-error';
    else if (e.level === 'WARN') rowClass += ' is-warn';
    else if (e.is_noise) rowClass += ' is-noise';

    row.className = rowClass;
    row.innerHTML = `
      <div class="col-line">L${e.line_number}</div>
      <div class="col-time">${e.raw_time ? e.raw_time.substring(11, 23) : '--'}</div>
      <div class="col-level"><span class="level-badge level-${e.level}">${e.level}</span></div>
      <div class="col-cat"><span class="cat-badge cat-${e.category}">${e.category}</span></div>
      <div class="col-comp" title="${e.component || ''}">${e.component || '--'}</div>
      <div class="col-msg" title="${escapeHtml(e.message)}">${escapeHtml(e.message)}</div>
      <div class="col-act">
        <button class="btn-linear-jump" title="Ver registros contiguos alrededor de esta línea">📍 Ver</button>
      </div>
    `;

    const jumpBtn = row.querySelector('.btn-linear-jump');
    if (jumpBtn) {
      jumpBtn.addEventListener('click', (ev) => {
        ev.stopPropagation();
        jumpToLinearContext(e.line_number);
      });
    }

    row.addEventListener('click', () => openModal(e));
    container.appendChild(row);
  });

  if (targetLine) {
    setTimeout(() => {
      const targetEl = container.querySelector('.is-target-context');
      if (targetEl) {
        targetEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    }, 100);
  }
}

function updatePagination(total, isContext = false, renderedCount = 0) {
  const controlsGroup = document.getElementById('pagination-controls-group');
  const countShowing = document.getElementById('page-showing-count');
  const countTotal = document.getElementById('page-total-count');
  const indicator = document.getElementById('page-current-indicator');
  const btnPrev = document.getElementById('btn-prev-page');
  const btnNext = document.getElementById('btn-next-page');

  if (isContext) {
    if (controlsGroup) controlsGroup.classList.add('is-disabled');
    if (countShowing) countShowing.textContent = `${renderedCount}`;
    if (countTotal) countTotal.textContent = total.toLocaleString();
    if (indicator) indicator.textContent = 'Contexto Lineal';
    if (btnPrev) btnPrev.disabled = true;
    if (btnNext) btnNext.disabled = true;
    return;
  }

  if (state.filters.all_rows) {
    if (controlsGroup) controlsGroup.classList.add('is-disabled');
    if (countShowing) countShowing.textContent = total === 0 ? '0' : `1-${renderedCount}`;
    if (countTotal) countTotal.textContent = total.toLocaleString();
    if (indicator) indicator.textContent = 'Todas las filas';
    if (btnPrev) btnPrev.disabled = true;
    if (btnNext) btnNext.disabled = true;
    return;
  }

  if (controlsGroup) controlsGroup.classList.remove('is-disabled');
  const maxPages = Math.ceil(total / state.pageSize);
  const start = total === 0 ? 0 : state.currentPage * state.pageSize + 1;
  const end = Math.min((state.currentPage + 1) * state.pageSize, total);

  if (countShowing) countShowing.textContent = total === 0 ? '0' : `${start}-${end}`;
  if (countTotal) countTotal.textContent = total.toLocaleString();
  if (btnPrev) btnPrev.disabled = state.currentPage === 0;
  if (btnNext) btnNext.disabled = state.currentPage + 1 >= maxPages;
  if (indicator) indicator.textContent = `Pág ${state.currentPage + 1} de ${Math.max(1, maxPages)}`;
}

// Modal Details Inspector
function initModal() {
  const modal = document.getElementById('detail-modal');
  const closeBtn = document.getElementById('btn-close-modal');
  const closeActionBtn = document.getElementById('btn-modal-close-action');
  const contextActionBtn = document.getElementById('btn-modal-context-action');
  const traceActionBtn = document.getElementById('btn-modal-trace-action');

  const closeModal = () => modal.classList.remove('active');
  closeBtn.addEventListener('click', closeModal);
  closeActionBtn.addEventListener('click', closeModal);
  modal.addEventListener('click', (e) => {
    if (e.target === modal) closeModal();
  });

  if (contextActionBtn) {
    contextActionBtn.addEventListener('click', () => {
      if (state.selectedEntry) {
        const lineNum = state.selectedEntry.line_number;
        closeModal();
        jumpToLinearContext(lineNum);
      }
    });
  }

  traceActionBtn.addEventListener('click', () => {
    if (state.selectedEntry) {
      const target = state.selectedEntry.dispatch_id || state.selectedEntry.guid || state.selectedEntry.pump_id;
      if (target) {
        closeModal();
        traceEntity(target);
      }
    }
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && modal.classList.contains('active')) {
      closeModal();
    }
  });
}

function openModal(entry) {
  state.selectedEntry = entry;
  const modal = document.getElementById('detail-modal');
  document.getElementById('modal-title').textContent = `Evento Línea #${entry.line_number}`;

  const metaGrid = document.getElementById('modal-meta');
  metaGrid.innerHTML = `
    <div class="meta-item"><div class="meta-label">Timestamp</div><div class="meta-val">${entry.raw_time}</div></div>
    <div class="meta-item"><div class="meta-label">Nivel</div><div class="meta-val"><span class="level-badge level-${entry.level}">${entry.level}</span></div></div>
    <div class="meta-item"><div class="meta-label">Categoría</div><div class="meta-val"><span class="cat-badge cat-${entry.category}">${entry.category}</span></div></div>
    <div class="meta-item"><div class="meta-label">Componente</div><div class="meta-val">${entry.component}</div></div>
    ${entry.guid ? `<div class="meta-item"><div class="meta-label">Correlation GUID</div><div class="meta-val">${entry.guid}</div></div>` : ''}
    ${entry.dispatch_id ? `<div class="meta-item"><div class="meta-label">Despacho ID</div><div class="meta-val">${entry.dispatch_id}</div></div>` : ''}
    ${entry.pump_id ? `<div class="meta-item"><div class="meta-label">Bomba</div><div class="meta-val">Bomba ${entry.pump_id}</div></div>` : ''}
    ${entry.error_reason ? `<div class="meta-item"><div class="meta-label">Motivo Error</div><div class="meta-val error-text">${entry.error_reason}</div></div>` : ''}
  `;

  document.getElementById('modal-code').textContent = entry.message;

  const traceBtn = document.getElementById('btn-modal-trace-action');
  const hasTarget = !!(entry.dispatch_id || entry.guid || entry.pump_id);
  traceBtn.style.display = hasTarget ? 'inline-block' : 'none';

  const aiBtn = document.getElementById('btn-modal-ai-action');
  if (aiBtn) {
    aiBtn.onclick = () => {
      modal.classList.remove('active');
      window.diagnoseLineWithAI(entry.line_number, entry.component || entry.dispatch_id || `#${entry.line_number}`);
    };
  }

  modal.classList.add('active');
}

// Global Quick Action: Trace Entity
window.traceEntity = function(target) {
  if (!target) return;
  switchTab('explorer');
  const inputQuery = document.getElementById('filter-query');
  inputQuery.value = target;
  document.getElementById('btn-clear-query').classList.add('visible');

  // Turn off hide noise so the user can see the complete lifecycle trace
  const checkHideNoise = document.getElementById('filter-hide-noise');
  checkHideNoise.checked = false;
  state.filters.hide_noise = false;

  state.filters.q = target;
  state.currentPage = 0;
  fetchLogs();
};

function escapeHtml(str) {
  if (!str) return '';
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

// ==========================================
// AI Debugger (Google Gemini)
// ==========================================
let currentAIContext = null;

function initAIDebugger() {
  const apiKeyInput = document.getElementById('ai-api-key');
  const modelSelect = document.getElementById('ai-model-select');
  const toggleEyeBtn = document.getElementById('btn-toggle-api-key');
  const statusBadge = document.getElementById('ai-key-status-badge');

  const anchorInput = document.getElementById('ai-search-anchor');
  const clearAnchorBtn = document.getElementById('btn-clear-ai-anchor');
  const btnExtract = document.getElementById('btn-ai-extract');
  const btnDiagnose = document.getElementById('btn-ai-diagnose');
  const btnCopyContext = document.getElementById('btn-copy-context');
  const btnCopyDiagnosis = document.getElementById('btn-copy-diagnosis');

  const stepPreview = document.getElementById('ai-step-preview');
  const stepDiagnosis = document.getElementById('ai-step-diagnosis');
  const codeViewer = document.getElementById('ai-code-viewer');
  const badgesContainer = document.getElementById('ai-preview-badges');
  const customPromptInput = document.getElementById('ai-custom-prompt');
  const tokenCostHint = document.getElementById('ai-token-cost-hint');
  const loadingState = document.getElementById('ai-loading-state');
  const diagnosisOutput = document.getElementById('ai-diagnosis-output');
  const diagnosisTitle = document.getElementById('ai-diagnosis-model-title');

  if (!apiKeyInput) return;

  // 1. Cargar clave y modelo guardados
  const savedKey = localStorage.getItem('interlog_gemini_key') || '';
  if (savedKey) {
    apiKeyInput.value = savedKey;
  }
  const savedModel = localStorage.getItem('interlog_gemini_model') || 'gemini-flash-lite-latest';
  if (modelSelect) {
    modelSelect.value = savedModel;
    modelSelect.addEventListener('change', () => {
      localStorage.setItem('interlog_gemini_model', modelSelect.value);
    });
  }

  // Comprobar estado del servidor (si tiene GEMINI_API_KEY en variables de entorno)
  fetch('/api/ai/status')
    .then(r => r.json())
    .then(data => {
      updateKeyStatus(data.has_env_key);
    })
    .catch(() => {
      updateKeyStatus(false);
    });

  function updateKeyStatus(hasServerEnvKey) {
    const hasLocalKey = !!apiKeyInput.value.trim();
    if (hasServerEnvKey) {
      statusBadge.innerHTML = `<span class="dot dot-emerald"></span><span>API Key activa en servidor (GEMINI_API_KEY)</span>`;
      statusBadge.classList.add('status-active');
    } else if (hasLocalKey) {
      statusBadge.innerHTML = `<span class="dot dot-emerald"></span><span>API Key guardada localmente</span>`;
      statusBadge.classList.add('status-active');
    } else {
      statusBadge.innerHTML = `<span class="dot dot-amber"></span><span>Sin API Key (Ingrésala abajo)</span>`;
      statusBadge.classList.remove('status-active');
    }
  }

  apiKeyInput.addEventListener('input', () => {
    const key = apiKeyInput.value.trim();
    if (key) {
      localStorage.setItem('interlog_gemini_key', key);
    } else {
      localStorage.removeItem('interlog_gemini_key');
    }
    updateKeyStatus(false);
  });

  // Toggle Ver/Ocultar API Key
  if (toggleEyeBtn) {
    toggleEyeBtn.addEventListener('click', () => {
      if (apiKeyInput.type === 'password') {
        apiKeyInput.type = 'text';
        toggleEyeBtn.textContent = '🔒';
      } else {
        apiKeyInput.type = 'password';
        toggleEyeBtn.textContent = '👁️';
      }
    });
  }

  // Clear Anchor
  if (clearAnchorBtn) {
    clearAnchorBtn.addEventListener('click', () => {
      anchorInput.value = '';
      anchorInput.focus();
    });
  }

  // Suggestion Chips
  document.querySelectorAll('.ai-chip').forEach(chip => {
    chip.addEventListener('click', () => {
      anchorInput.value = chip.dataset.anchor;
      triggerAIExtract();
    });
  });

  // Enter key on anchor input
  if (anchorInput) {
    anchorInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        triggerAIExtract();
      }
    });
  }

  // Action: Trigger Extraction (Paso 1)
  if (btnExtract) {
    btnExtract.addEventListener('click', () => {
      triggerAIExtract();
    });
  }

  window.triggerAIExtract = function(customAnchor) {
    if (customAnchor !== undefined) {
      anchorInput.value = customAnchor;
    }
    const anchor = anchorInput.value.trim();
    if (!anchor) {
      alert('Por favor ingresa un término de búsqueda, componente o número de línea.');
      anchorInput.focus();
      return;
    }

    const linesBefore = parseInt(document.getElementById('ai-lines-before').value) || 15;
    const linesAfter = parseInt(document.getElementById('ai-lines-after').value) || 15;
    const maxScan = parseInt(document.getElementById('ai-max-scan').value) || 500;
    const hideNoise = document.getElementById('ai-hide-noise').checked;

    btnExtract.disabled = true;
    btnExtract.innerHTML = `<span class="ai-spinner" style="width:16px;height:16px;border-width:2px;display:inline-block;"></span><span>Buscando error...</span>`;

    fetch('/api/ai/extract', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        anchor: anchor,
        lines_before: linesBefore,
        lines_after: linesAfter,
        max_scan: maxScan,
        hide_noise: hideNoise
      })
    })
    .then(r => r.json())
    .then(res => {
      btnExtract.disabled = false;
      btnExtract.innerHTML = `<svg viewBox="0 0 24 24" width="16" height="16" stroke="currentColor" stroke-width="2" fill="none"><polyline points="7 13 12 18 17 13"></polyline><polyline points="7 6 12 11 17 6"></polyline></svg><span>Buscar y Extraer Contexto</span>`;

      if (!res.success && !res.anchor_found) {
        alert(res.message || 'No se encontró el ancla especificada en el archivo actual.');
        return;
      }

      currentAIContext = res;
      renderExtractedPreview(res);
    })
    .catch(err => {
      btnExtract.disabled = false;
      btnExtract.innerHTML = `<svg viewBox="0 0 24 24" width="16" height="16" stroke="currentColor" stroke-width="2" fill="none"><polyline points="7 13 12 18 17 13"></polyline><polyline points="7 6 12 11 17 6"></polyline></svg><span>Buscar y Extraer Contexto</span>`;
      alert('Error de conexión al extraer contexto: ' + err.message);
    });
  };

  function renderExtractedPreview(res) {
    stepPreview.classList.remove('hidden');

    let errorBadge = '';
    if (res.error_found) {
      const distSign = res.error_distance > 0 ? `(+${res.error_distance} líneas)` : '(en la misma línea)';
      errorBadge = `<span class="ai-metric-pill pill-rose">🚨 Error en L#${res.error_line} ${distSign}</span>`;
    } else {
      errorBadge = `<span class="ai-metric-pill pill-amber">⚠️ Sin error explícito en ${res.lines_after || 500} líneas</span>`;
    }

    badgesContainer.innerHTML = `
      <span class="ai-metric-pill pill-cyan">🎯 Ancla en L#${res.anchor_line}</span>
      ${errorBadge}
      <span class="ai-metric-pill pill-emerald">🏷️ ${escapeHtml(res.error_reason || 'Contexto')}</span>
      <span class="ai-metric-pill">🪙 ~${res.estimated_tokens} tokens</span>
    `;

    codeViewer.textContent = res.context_text;
    tokenCostHint.textContent = `Consumo aproximado: ~${res.estimated_tokens} tokens (~$${(res.estimated_tokens * 0.00000015).toFixed(6)} USD)`;

    stepPreview.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  // Action: Trigger Diagnosis (Paso 2 -> 3)
  if (btnDiagnose) {
    btnDiagnose.addEventListener('click', () => {
      if (!currentAIContext || !currentAIContext.context_text) {
        alert('Primero debes buscar y extraer un bloque de contexto.');
        return;
      }

      const apiKey = apiKeyInput.value.trim();
      const model = modelSelect ? modelSelect.value : 'gemini-2.5-flash';
      const customPrompt = customPromptInput ? customPromptInput.value.trim() : '';

      stepDiagnosis.classList.remove('hidden');
      loadingState.classList.remove('hidden');
      diagnosisOutput.innerHTML = '';
      btnDiagnose.disabled = true;

      stepDiagnosis.scrollIntoView({ behavior: 'smooth', block: 'nearest' });

      fetch('/api/ai/diagnose', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Gemini-Key': apiKey
        },
        body: JSON.stringify({
          context_text: currentAIContext.context_text,
          custom_prompt: customPrompt,
          api_key: apiKey,
          model: model
        })
      })
      .then(async r => {
        const data = await r.json();
        if (!r.ok || !data.success) {
          throw new Error(data.error || 'Error al comunicarse con Gemini');
        }
        return data;
      })
      .then(data => {
        loadingState.classList.add('hidden');
        btnDiagnose.disabled = false;
        diagnosisTitle.textContent = `Diagnóstico (${data.model} - ${data.latency_ms}ms)`;
        diagnosisOutput.innerHTML = renderSimpleMarkdown(data.analysis);
        stepDiagnosis.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      })
      .catch(err => {
        loadingState.classList.add('hidden');
        btnDiagnose.disabled = false;
        diagnosisOutput.innerHTML = `
          <div style="background: rgba(244, 63, 94, 0.1); border: 1px solid rgba(244, 63, 94, 0.3); padding: 1.25rem; border-radius: 8px; color: #fecdd3;">
            <h4 style="color: #f43f5e; margin-bottom: 0.5rem;">❌ Error al generar diagnóstico</h4>
            <p>${escapeHtml(err.message)}</p>
            <p style="margin-top: 0.75rem; font-size: 0.85rem; color: #94a3b8;">
              Asegúrate de ingresar una API Key válida de <a href="https://aistudio.google.com/app/apikey" target="_blank" style="color: #38bdf8;">Google AI Studio</a>.
            </p>
          </div>
        `;
      });
    });
  }

  // Copiar Contexto
  if (btnCopyContext) {
    btnCopyContext.addEventListener('click', () => {
      if (!currentAIContext) return;
      navigator.clipboard.writeText(currentAIContext.context_text).then(() => {
        const origText = btnCopyContext.innerHTML;
        btnCopyContext.innerHTML = `<span>✓ ¡Copiado!</span>`;
        setTimeout(() => { btnCopyContext.innerHTML = origText; }, 2000);
      });
    });
  }

  // Copiar Diagnóstico
  if (btnCopyDiagnosis) {
    btnCopyDiagnosis.addEventListener('click', () => {
      const text = diagnosisOutput.innerText;
      if (!text) return;
      navigator.clipboard.writeText(text).then(() => {
        const origText = btnCopyDiagnosis.innerHTML;
        btnCopyDiagnosis.innerHTML = `<span>✓ ¡Copiado!</span>`;
        setTimeout(() => { btnCopyDiagnosis.innerHTML = origText; }, 2000);
      });
    });
  }
}

// Global action called from row details or anywhere
window.diagnoseLineWithAI = function(lineNum, component) {
  switchTab('ai');
  const anchorInput = document.getElementById('ai-search-anchor');
  if (anchorInput) {
    anchorInput.value = component || '#' + lineNum;
  }
  if (window.triggerAIExtract) {
    window.triggerAIExtract(component || '#' + lineNum);
  }
};

// Mini Markdown Renderer
function renderSimpleMarkdown(md) {
  if (!md) return '';
  let lines = md.split('\n');
  let html = '';
  let inList = false;
  let inCodeBlock = false;
  let codeBlockContent = '';

  for (let i = 0; i < lines.length; i++) {
    let line = lines[i];

    // Code blocks ```
    if (line.trim().startsWith('```')) {
      if (inCodeBlock) {
        html += `<pre><code>${escapeHtml(codeBlockContent.trim())}</code></pre>`;
        codeBlockContent = '';
        inCodeBlock = false;
      } else {
        if (inList) { html += '</ul>'; inList = false; }
        inCodeBlock = true;
      }
      continue;
    }

    if (inCodeBlock) {
      codeBlockContent += line + '\n';
      continue;
    }

    // Headers
    if (line.startsWith('### ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += `<h3>${formatInline(line.substring(4))}</h3>`;
      continue;
    }
    if (line.startsWith('## ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += `<h3>${formatInline(line.substring(3))}</h3>`;
      continue;
    }
    if (line.startsWith('# ')) {
      if (inList) { html += '</ul>'; inList = false; }
      html += `<h3>${formatInline(line.substring(2))}</h3>`;
      continue;
    }

    // Bullet list
    const trimmed = line.trim();
    if (trimmed.startsWith('* ') || trimmed.startsWith('- ') || /^\d+\.\s/.test(trimmed)) {
      if (!inList) {
        html += '<ul>';
        inList = true;
      }
      let content = trimmed.replace(/^(\*|-|\d+\.)\s+/, '');
      html += `<li>${formatInline(content)}</li>`;
      continue;
    } else {
      if (inList) {
        html += '</ul>';
        inList = false;
      }
    }

    // Empty line
    if (!trimmed) {
      continue;
    }

    // Paragraph
    html += `<p>${formatInline(trimmed)}</p>`;
  }

  if (inList) html += '</ul>';
  if (inCodeBlock) html += `<pre><code>${escapeHtml(codeBlockContent.trim())}</code></pre>`;

  return html;

  function formatInline(text) {
    let out = escapeHtml(text);
    // Bold **text**
    out = out.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
    // Inline code `code`
    out = out.replace(/`([^`]+)`/g, '<code>$1</code>');
    return out;
  }
}
