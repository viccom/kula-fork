/* ============================================================
   processes.js — Process monitor page.

   Renders /api/processes as a table with a free-text filter
   (name / PID / user) and a state filter. The page is opened by
   the header button and driven by monitor.js.
   ============================================================ */
'use strict';
import { createMonitorPage, buildTable, buildSelect, buildSearch, toggledSort, sortRows, node, bytes, formatTime } from './monitor.js';
import { i18n } from './i18n.js';

const processStates = ['running', 'sleeping', 'idle', 'disk_sleep', 'zombie', 'stopped', 'paging', 'parked', 'dead', 'unknown'];
const maxRows = 1000;

let latest = null;
let query = '';
let stateFilter = '';
let filtersBuilt = false;
// Metrics start descending so the busiest process is on top; the name column
// starts ascending, which is what a reader expects from "sort by name".
let sort = { key: 'cpu_pct', dir: 'desc' };

const comparators = {
    name: (a, b) => String(a.name || '').localeCompare(String(b.name || '')),
    cpu_pct: (a, b) => (a.cpu_pct || 0) - (b.cpu_pct || 0),
    mem_rss: (a, b) => (a.mem_rss || 0) - (b.mem_rss || 0),
};

const stateLabel = state => i18n.t(`monitor_state_${state}`);

function onSort(key) {
    sort = toggledSort(sort, key, columns());
    renderTable();
}

function columns() {
    return [
        { label: i18n.t('monitor_pid'), numeric: true, value: p => p.pid },
        { key: 'name', label: i18n.t('monitor_name'), value: p => p.name || '—', sortable: true, defaultDir: 'asc' },
        { label: i18n.t('monitor_ppid'), numeric: true, value: p => p.ppid },
        { label: i18n.t('monitor_threads'), numeric: true, value: p => p.threads },
        { label: i18n.t('monitor_user'), value: p => p.user || '—' },
        { key: 'cpu_pct', label: 'CPU', numeric: true, value: p => `${(p.cpu_pct || 0).toFixed(1)}%`, sortable: true, defaultDir: 'desc' },
        { key: 'mem_rss', label: i18n.t('monitor_memory'), numeric: true, value: p => bytes(p.mem_rss), sortable: true, defaultDir: 'desc' },
        { label: i18n.t('monitor_conns'), numeric: true, value: p => p.sockets },
        { label: i18n.t('monitor_state'), value: p => stateLabel(p.state) },
        { label: i18n.t('monitor_started'), value: p => formatTime(p.started) },
    ];
}

function visibleRows() {
    const term = query.trim().toLowerCase();
    const rows = (latest?.processes || []).filter(p => {
        if (stateFilter && p.state !== stateFilter) return false;
        if (!term) return true;
        return String(p.pid).includes(term) ||
            String(p.name || '').toLowerCase().includes(term) ||
            String(p.user || '').toLowerCase().includes(term);
    });
    return sortRows(rows, sort, comparators, (a, b) => a.pid - b.pid);
}

function buildFilters() {
    if (filtersBuilt) return;
    const host = document.getElementById('processes-filters');
    if (!host) return;
    filtersBuilt = true;
    const pairs = [['', i18n.t('monitor_all_states')], ...processStates.map(state => [state, stateLabel(state)])];
    host.replaceChildren(
        buildSearch(i18n.t('monitor_search_process'), query, value => { query = value; renderTable(); }),
        buildSelect('monitor-select', pairs, stateFilter, value => { stateFilter = value; renderTable(); }),
    );
}

function renderTable() {
    const content = document.getElementById('processes-content');
    if (!content) return;
    const rows = visibleRows();
    const shown = rows.slice(0, maxRows);
    const count = node('p', 'monitor-count',
        `${i18n.t('monitor_rows')} ${shown.length}${rows.length > shown.length ? ` / ${rows.length}` : ''}`);
    const wrap = node('div', 'monitor-table-wrap');
    wrap.append(buildTable(columns(), shown, { sort, onSort }));
    content.replaceChildren(count, wrap);
}

function render(snapshot) {
    latest = snapshot;
    buildFilters();
    renderTable();
}

export function initProcesses() {
    createMonitorPage({
        page: 'processes-page', button: 'btn-processes', back: 'processes-back',
        refresh: 'processes-refresh', status: 'processes-status',
        content: 'processes-content', filters: 'processes-filters',
        route: '#processes', fetchPath: '/api/processes', render,
    }).init();
}
