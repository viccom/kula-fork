/* ============================================================
   pages.js — Exclusive registry for the dashboard's full-screen pages.

   The system-info page and the two monitor pages share one viewport.
   Rather than each page trying to close the others through the
   others' header buttons, every page registers here and calls
   openPage() before it appears; openPage() then closes each other
   registered page through its own close handler. That keeps the
   pages mutually exclusive no matter which order they are opened in
   (including via history navigation).
   ============================================================ */
'use strict';

const pages = new Map();

// registerPage adds a page to the exclusive set. isOpen reports whether the
// page is currently visible; close hides it without navigating history — the
// page that is opening owns the history entry.
export function registerPage(id, { isOpen, close }) {
    pages.set(id, { isOpen, close });
}

// openPage closes every other registered page. Call it before revealing a page.
export function openPage(id) {
    for (const [other, page] of pages) {
        if (other !== id && page.isOpen()) page.close();
    }
}
