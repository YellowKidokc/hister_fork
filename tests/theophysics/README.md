These tests use the running control center, real PostgreSQL, and Hister. They add
research jobs and retained documents. Use disposable development data and an
unconfigured AI provider for the no-provider checks.

From the repository root, install the existing npm lockfile (`npm ci`), start the
three services described in deploy/theophysics/README.md, and run:

```sh
TP_E2E_TOKEN='your-development-control-center-token' \
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium \
npx playwright test --config tests/theophysics/playwright.config.ts
```

Set TP_E2E_URL if the API uses an address other than http://127.0.0.1:4444. Omit
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH to use Playwright's installed browser. Never
put credentials in tracked test files. Desktop and mobile tests cover login,
folder uploads, exact/probable duplicate review, manual classification, commit,
index readiness, search, Markdown export, source download, unknown-category
fallback, and extraction errors. Screenshots/results live under ignored .test/.

The container deployment smoke test uses only Python's standard library:

```sh
TP_E2E_URL='http://127.0.0.1:4444' TP_E2E_TOKEN='your-test-token' \
python3 tests/theophysics/deployment-smoke.py
```

It creates one clearly named validation job and retains its source/receipts.
Use a disposable stack. It checks exact duplicate identity, commit, actual Hister
search, export provenance/hash, and source download. It does not deploy anything
or change storage paths.
