# Legal Documents Maintenance

> This page is a concise English guide. The complete, authoritative document is available in Chinese — switch the language toggle at the top right to read the full version.

The Terms of Service and Privacy Policy texts live in `web/src/content/legal.ts` and are rendered by `LegalView.vue` on the site.

## Before going live

Replace the placeholder operator name, contact email and domain in `legalMeta` with your own details. The shipped values are examples only and must not be deployed as-is.

## What the Chinese document covers

- A mapping table between policy clauses and the code that implements them (data collection, retention, cookies, third-party services).
- Compliance notes for different jurisdictions (e.g. GDPR-style data subject rights).
- A checklist for reviewing the text when you change data handling, storage providers or email providers.

This repository ships no legal advice. Have qualified counsel review the final text for your operating region.
