/* ============================================================
   connections.js — Network monitor page.

   Renders /api/connections as a table with protocol, state and
   free-text filters (process / PID / port). Opened by the header
   button and driven by monitor.js.
   ============================================================ */
'use strict';
import { createMonitorPage, buildTable, buildSelect, buildSearch, node } from './monitor.js';
import { i18n } from './i18n.js';

const protocolOrder = ['tcp', 'tcp6', 'udp', 'udp6'];
const connectionStates = ['listen', 'established', 'syn_sent', 'syn_recv', 'fin_wait1', 'fin_wait2',
    'time_wait', 'close', 'close_wait', 'last_ack', 'closing', 'new_syn_recv', 'unknown'];
const maxRows = 1000;

let latest = null;
let query = '';
let protoFilter = '';
let stateFilter = '';
let filtersBuilt = false;

const stateLabel = state => i18n.t(`conn_state_${state}`);
const protoLabel = proto => i18n.t(`monitor_proto_${proto}`);

const endpoint = (address, port) => `${address || '—'}:${port}`;

function columns() {
    return [
        { label: i18n.t('monitor_proto'), value: c => protoLabel(c.proto) },
        { label: i18n.t('monitor_pid'), numeric: true, value: c => c.pid || '—' },
        { label: i18n.t('monitor_process'), value: c => c.process || '—' },
        { label: i18n.t('monitor_local'), value: c => endpoint(c.local_addr, c.local_port) },
        { label: i18n.t('monitor_remote'), value: c => endpoint(c.remote_addr, c.remote_port) },
        { label: i18n.t('monitor_state'), value: c => stateLabel(c.state) },
    ];
}

function visibleRows() {
    const term = query.trim().toLowerCase();
    return (latest?.connections || []).filter(c => {
        if (protoFilter && c.proto !== protoFilter) return false;
        if (stateFilter && c.state !== stateFilter) return false;
        if (!term) return true;
        return String(c.pid || '').includes(term) ||
            String(c.process || '').toLowerCase().includes(term) ||
            String(c.local_port || '').includes(term) ||
            String(c.remote_port || '').includes(term) ||
            String(c.local_addr || '').includes(term) ||
            String(c.remote_addr || '').includes(term);
    }).sort((a, b) => protocolOrder.indexOf(a.proto) - protocolOrder.indexOf(b.proto) ||
        (a.local_port || 0) - (b.local_port || 0));
}

function buildFilters() {
    if (filtersBuilt) return;
    const host = document.getElementById('connections-filters');
    if (!host) return;
    filtersBuilt = true;
    const protoPairs = [['', i18n.t('monitor_all_protocols')], ...protocolOrder.map(proto => [proto, protoLabel(proto)])];
    const statePairs = [['', i18n.t('monitor_all_states')], ...connectionStates.map(state => [state, stateLabel(state)])];
    host.replaceChildren(
        buildSelect('monitor-select', protoPairs, protoFilter, value => { protoFilter = value; renderTable(); }),
        buildSelect('monitor-select', statePairs, stateFilter, value => { stateFilter = value; renderTable(); }),
        buildSearch(i18n.t('monitor_search_connection'), query, value => { query = value; renderTable(); }),
    );
}

function renderTable() {
    const content = document.getElementById('connections-content');
    if (!content) return;
    const rows = visibleRows();
    const shown = rows.slice(0, maxRows);
    const count = node('p', 'monitor-count',
        `${i18n.t('monitor_rows')} ${shown.length}${rows.length > shown.length ? ` / ${rows.length}` : ''}`);
    const wrap = node('div', 'monitor-table-wrap');
    wrap.append(buildTable(columns(), shown));
    content.replaceChildren(count, wrap);
}

function render(snapshot) {
    latest = snapshot;
    buildFilters();
    renderTable();
}

export function initConnections() {
    createMonitorPage({
        page: 'connections-page', button: 'btn-connections', back: 'connections-back',
        refresh: 'connections-refresh', status: 'connections-status',
        content: 'connections-content', filters: 'connections-filters',
        route: '#connections', fetchPath: '/api/connections', render,
    }).init();
}
