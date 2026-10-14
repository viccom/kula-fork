/* ============================================================
   monitor.js — Shared lifecycle and table helpers for the
   process and network monitor pages.

   Each monitor page is a full-screen sibling of the dashboard
   and the system-info page. The controller here owns the parts
   they share: open/close with hash routing, polling, the update
   status line, and building a sortable/filterable table. The
   page module supplies only the endpoint and a render callback.
   ============================================================ */
'use strict';
import { apiUrl } from './api.js';
import { i18n } from './i18n.js';
import { formatBytesShort } from './format.js';
import { openPage, registerPage } from './pages.js';

const pollInterval = 3000;

export function node(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = String(text);
    return element;
}

export function bytes(value) {
    return typeof value === 'number' && Number.isFinite(value) ? formatBytesShort(value) : '—';
}

export function formatTime(seconds) {
    if (!seconds) return '—';
    const date = new Date(seconds * 1000);
    return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString(i18n.currentLang);
}

// buildTable renders a header row and body rows from a column spec. Columns
// with numeric:true get a right-aligned numeric cell. A column marked
// sortable:true renders its header as a button that calls onSort(key); the
// active column and direction are exposed through aria-sort so assistive
// technology reports the table's ordering too.
export function buildTable(columns, rows, { sort, onSort } = {}) {
    const table = node('table', 'monitor-table');
    const thead = node('thead');
    const headRow = node('tr');
    for (const column of columns) {
        const th = node('th', column.numeric ? 'is-numeric' : '');
        th.scope = 'col';
        if (column.sortable && onSort) {
            const active = !!sort && sort.key === column.key;
            const ascending = active && sort.dir === 'asc';
            th.classList.add('is-sortable');
            th.setAttribute('aria-sort', active ? (ascending ? 'ascending' : 'descending') : 'none');
            const button = node('button', 'monitor-sort');
            button.type = 'button';
            button.append(node('span', '', column.label));
            button.append(node('span', 'monitor-sort-ind', active ? (ascending ? '▲' : '▼') : '↕'));
            button.addEventListener('click', () => onSort(column.key));
            th.append(button);
        } else {
            th.textContent = column.label;
        }
        headRow.append(th);
    }
    thead.append(headRow);
    const tbody = node('tbody');
    for (const row of rows) {
        const tr = node('tr');
        for (const column of columns) {
            const value = column.value(row);
            tr.append(node('td', column.numeric ? 'is-numeric' : '', value === undefined || value === null ? '—' : value));
        }
        tbody.append(tr);
    }
    table.append(thead, tbody);
    return table;
}

// toggledSort returns the next sort state when a header is clicked: the same
// column flips direction, a new column starts in its declared default
// direction (descending for metrics, ascending for names).
export function toggledSort(current, key, columns) {
    if (current && current.key === key) {
        return { key, dir: current.dir === 'asc' ? 'desc' : 'asc' };
    }
    const column = columns.find(c => c.key === key);
    return { key, dir: column && column.defaultDir ? column.defaultDir : 'asc' };
}

// sortRows orders rows by the comparator registered for sort.key; a stable
// tie-break keeps equal values in their previous order.
export function sortRows(rows, sort, comparators, tieBreak) {
    if (!sort || !sort.key || !comparators[sort.key]) return rows;
    const compare = comparators[sort.key];
    const direction = sort.dir === 'asc' ? 1 : -1;
    return rows.sort((a, b) => direction * compare(a, b) || (tieBreak ? tieBreak(a, b) : 0));
}

// buildSelect creates a labelled <select> whose options are value/label pairs.
export function buildSelect(className, pairs, current, onChange) {
    const select = node('select', className);
    for (const [value, label] of pairs) {
        const option = node('option', '', label);
        option.value = value;
        if (value === current) option.selected = true;
        select.append(option);
    }
    select.addEventListener('change', () => onChange(select.value));
    return select;
}

export function buildSearch(placeholder, current, onInput) {
    const input = node('input', 'monitor-search');
    input.type = 'search';
    input.placeholder = placeholder;
    input.autocomplete = 'off';
    input.spellcheck = false;
    input.value = current;
    input.addEventListener('input', () => onInput(input.value));
    return input;
}

// createMonitorPage wires one monitor page. render(snapshot) must repaint the
// content region; it is called on every successful poll and on language change.
export function createMonitorPage({ page, button, back, refresh, status, content, filters, route, fetchPath, render }) {
    const el = id => document.getElementById(id);
    const pageEl = () => el(page);
    const routeId = route.replace('#', '');
    const isOpen = () => { const p = pageEl(); return !!p && !p.classList.contains('hidden'); };
    let snapshot = null;
    let timer = null;
    let request = null;
    let enabled = true;
    let dashboardScroll = 0;

    const setStatus = (text, tone = '') => {
        const target = el(status);
        if (!target) return;
        target.textContent = text;
        target.className = `monitor-status${tone ? ` is-${tone}` : ''}`;
    };

    const stop = () => {
        clearTimeout(timer);
        timer = null;
        request?.abort();
        request = null;
    };

    const statusLine = () => {
        if (!snapshot) return;
        const updated = new Date(snapshot.ts);
        const time = Number.isNaN(updated.getTime()) ? '—' : updated.toLocaleTimeString(i18n.currentLang);
        setStatus(`${i18n.t('monitor_updated')} ${time}`, 'live');
    };

    async function refreshNow() {
        if (!enabled || !isOpen() || document.hidden || request) return;
        clearTimeout(timer);
        const controller = new AbortController();
        request = controller;
        const timeout = setTimeout(() => controller.abort(), 15000);
        el(refresh).disabled = true;
        setStatus(i18n.t(snapshot ? 'monitor_refreshing' : 'monitor_loading'), 'loading');
        try {
            const response = await fetch(apiUrl(fetchPath), { cache: 'no-store', signal: controller.signal });
            if (controller !== request) return;
            if ([401, 403, 404].includes(response.status)) { hide({ useHistory: false, restoreScroll: false }); return; }
            if (!response.ok) throw new Error('request failed');
            snapshot = await response.json();
            if (controller !== request) return;
            statusLine();
            render(snapshot);
        } catch (_error) {
            if (controller === request) {
                setStatus(i18n.t(snapshot ? 'monitor_refresh_failed' : 'monitor_load_failed'), 'error');
            }
        } finally {
            clearTimeout(timeout);
            if (controller === request) {
                request = null;
                el(refresh).disabled = false;
                if (isOpen() && !document.hidden) timer = setTimeout(refreshNow, pollInterval);
            }
        }
    }

    // show mounts the page, releasing every other full-screen page through the
    // shared registry first so only one is ever visible.
    function show({ pushRoute = true } = {}) {
        if (!enabled || isOpen()) return;
        dashboardScroll = window.scrollY;
        // Only one full-screen page may be mounted; release every other page.
        openPage(page);
        el('dashboard')?.classList.add('monitor-active');
        el('dashboard-home')?.classList.add('hidden');
        pageEl()?.classList.remove('hidden');
        pageEl()?.removeAttribute('aria-hidden');
        el(button)?.classList.add('monitor-current');
        el(button)?.setAttribute('aria-expanded', 'true');
        if (pushRoute && window.location.hash !== route) {
            window.history.pushState({ kulaPage: routeId }, '', route);
        }
        window.scrollTo({ top: 0, behavior: 'instant' });
        refreshNow();
    }

    function hide({ useHistory = true, restoreScroll = true } = {}) {
        const wasOpen = isOpen();
        stop();
        pageEl()?.classList.add('hidden');
        pageEl()?.setAttribute('aria-hidden', 'true');
        el(button)?.classList.remove('monitor-current');
        el(button)?.setAttribute('aria-expanded', 'false');
        el('dashboard')?.classList.remove('monitor-active');
        el('dashboard-home')?.classList.remove('hidden');
        if (wasOpen && restoreScroll) window.scrollTo({ top: dashboardScroll, behavior: 'instant' });
        if (window.location.hash === route) {
            if (useHistory && window.history.state?.kulaPage === routeId) window.history.back();
            else window.history.replaceState(null, '', window.location.pathname + window.location.search);
        }
    }

    function init() {
        registerPage(page, {
            isOpen,
            close: () => hide({ useHistory: false, restoreScroll: false }),
        });
        el(button)?.addEventListener('click', () => isOpen() ? hide() : show());
        el(back)?.addEventListener('click', () => hide());
        el(refresh)?.addEventListener('click', refreshNow);
        window.addEventListener('hashchange', () => {
            if (window.location.hash === route && enabled) show({ pushRoute: false });
            else if (isOpen()) hide({ useHistory: false });
        });
        document.addEventListener('visibilitychange', () => { if (document.hidden) stop(); else refreshNow(); });
        document.addEventListener('kula-config-ready', event => {
            enabled = event.detail.show_system_info !== false;
            el(button)?.classList.toggle('hidden', !enabled);
            if (!enabled) hide({ useHistory: false, restoreScroll: false });
        });
        document.addEventListener('kula-i18n-changed', () => {
            if (snapshot) render(snapshot);
            statusLine();
        });
        el(filters)?.replaceChildren();
        el(content)?.replaceChildren();
    }

    return { init, refresh: refreshNow, isOpen };
}
