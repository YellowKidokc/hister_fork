import { test, expect } from '@playwright/test';
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const token = process.env.TP_E2E_TOKEN;
test.skip(!token, 'Set TP_E2E_TOKEN for the running development control center.');

test('authentication protects the workspace', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Your research, in one place.' })).toBeVisible();
  await page.getByLabel('Control center access token').fill('incorrect');
  await page.getByRole('button', { name: 'Connect to library' }).click();
  await expect(page.getByRole('alert')).toContainText('access token');
  await page.getByLabel('Control center access token').fill(token!);
  await page.getByRole('button', { name: 'Connect to library' }).click();
  await expect(page.getByRole('heading', { name: 'Research jobs', exact: true })).toBeVisible();
  const response = await page.request.get('/v1/jobs');
  expect(response.status()).toBe(401);
});

test.describe('research workspace', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript((value) => sessionStorage.setItem('tp-token', value!), token);
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Research jobs', exact: true })).toBeVisible();
  });

  test('folder intake preserves duplicates, human labels, search, and export', async ({ page }) => {
    const dir = await mkdtemp(join(tmpdir(), 'tp-browser-'));
    const folder = join(dir, 'Research collection');
    const term = `browserprobe${Date.now()}${test.info().project.name}`;
    const name = `Equation research ${term}`;
    const original = `# Conserved information\n\n${term} connects physics and information. This source supports careful research.\n`;
    try {
      await mkdir(join(folder, 'nested'), { recursive: true });
      await writeFile(join(folder, 'original.md'), original);
      await writeFile(join(folder, 'nested', 'exact-copy.md'), original);
      await writeFile(
        join(folder, 'normalized.txt'),
        original.toLowerCase().replaceAll(/[^a-z0-9 ]/g, ' '),
      );
      await page.getByRole('button', { name: '+ Create job' }).click();
      await page.getByLabel('Give this collection a name').fill(name);
      await page.locator('#initial-categories').getByLabel('Physics', { exact: true }).check();
      await page.locator('#initial-categories').getByLabel('Information', { exact: true }).check();
      await page.locator('#create-dialog').getByRole('button', { name: 'Create job →' }).click();
      await expect(page.locator('#job-title')).toHaveText(name);
      await page.locator('#folder').setInputFiles(folder);
      await expect(page.locator('#upload-label')).toHaveText('3 accepted · 0 failed', {
        timeout: 30000,
      });
      await expect(page.locator('#job-summary')).toContainText(
        '2 canonical documents · 3 source receipts · 1 exact duplicates',
      );
      await expect(page.locator('#job-documents')).toContainText('probable matches');
      await page
        .locator('#job-documents')
        .getByRole('button', { name: 'Review labels' })
        .first()
        .click();
      await expect(page.locator('#detail-meta')).toContainText('SHA-256:');
      await expect(page.locator('#detail-receipts')).toContainText('Research collection');
      await expect(page.locator('#detail-text')).toContainText(term);
      await page
        .locator('#detail-dialog')
        .getByRole('button', { name: 'Done', exact: true })
        .click();
      await page.getByRole('button', { name: 'Commit selected classifications →' }).click();
      await expect(page.locator('#job-state')).toHaveText('committed');
      await expect(
        page.locator('#job-documents .badge').filter({ hasText: /^indexed$/ }),
      ).toHaveCount(2, { timeout: 20000 });
      await page.getByRole('button', { name: 'Search library', exact: true }).click();
      await page.locator('#search-query').fill(term);
      await page.locator('#filter-job').selectOption({ label: name });
      await page.locator('#filter-category').selectOption('physics');
      await page
        .locator('#search-form')
        .getByRole('button', { name: 'Search', exact: true })
        .click();
      await expect(page.locator('#search-results .result-card')).toHaveCount(2, { timeout: 15000 });
      await page.locator('#search-results input[type=checkbox]').first().check();
      const downloadPromise = page.waitForEvent('download');
      await page.getByRole('button', { name: 'Export selected as Markdown' }).click();
      const download = await downloadPromise;
      const output = await readFile((await download.path())!, 'utf8');
      expect(output).toContain(term);
      expect(output).toContain('SHA-256:');
      expect(output).toContain('Source receipts:');
      expect(output).toContain('physics');
      await page.getByRole('button', { name: 'Activity & receipts', exact: true }).click();
      await expect(page.locator('#events')).toContainText('export.created');
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  test('unknown material stays in Inbox and Workbench, with no AI guess', async ({ page }) => {
    const name = `Unclassified ${Date.now()} ${test.info().project.name}`;
    await page.getByRole('button', { name: '+ Create job' }).click();
    await page.getByLabel('Give this collection a name').fill(name);
    await page.locator('#create-dialog').getByRole('button', { name: 'Create job →' }).click();
    await expect(page.locator('#job-state')).toHaveText('review');
    await expect(page.locator('#choose-files')).toBeEnabled();
    await page.locator('#files').setInputFiles({
      name: 'unclassified.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from(name + ' is intentionally unclassified.'),
    });
    await expect(page.locator('#upload-label')).toHaveText('1 accepted · 0 failed');
    await expect(page.locator('#job-documents .chips')).toContainText('Inbox');
    await expect(page.locator('#job-documents .chips')).toContainText('Workbench');
    await expect(page.getByRole('button', { name: 'Suggest with AI' })).toBeDisabled();
    await page.getByRole('button', { name: 'Use confidence policy' }).click();
    await expect(page.locator('#job-state')).toHaveText('committed');
    await expect(page.locator('#job-documents .chips')).toContainText('Inbox');
    await page.getByRole('button', { name: 'Classification review', exact: true }).click();
    await expect(page.locator('#review-queue')).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await page.screenshot({
      path: `.test/theophysics-${test.info().project.name}.png`,
      fullPage: true,
    });
  });

  test('unsupported bytes retain their source and cannot be committed', async ({ page }) => {
    await page.getByRole('button', { name: '+ Create job' }).click();
    await page.getByLabel('Give this collection a name').fill(`Binary review ${Date.now()}`);
    await page.locator('#create-dialog').getByRole('button', { name: 'Create job →' }).click();
    await expect(page.locator('#job-state')).toHaveText('review');
    await expect(page.locator('#choose-files')).toBeEnabled();
    await page.locator('#files').setInputFiles({
      name: 'raw.bin',
      mimeType: 'application/octet-stream',
      buffer: Buffer.from([0xff, 0xfe, 0xfd]),
    });
    await expect(page.locator('#upload-label')).toHaveText('1 accepted · 0 failed');
    await expect(page.locator('#job-documents')).toContainText('Needs extraction review');
    await expect(
      page.getByRole('button', { name: 'Commit selected classifications →' }),
    ).toBeDisabled();
    await page.locator('#job-documents .doc-name').click();
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Download preserved source' }).click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe('raw.bin');
    expect(await readFile((await download.path())!)).toEqual(Buffer.from([0xff, 0xfe, 0xfd]));
  });
});

test('selected library files have a saved move preview without moving data', async ({ page }) => {
  await page.addInitScript((value) => sessionStorage.setItem('tp-token', value!), token);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Research jobs', exact: true })).toBeVisible();
  const term = `storageprobe${Date.now()}${test.info().project.name}`;
  const jobName = `Storage review ${term}`;
  const source = Buffer.from(`Preserved research source ${term}.`);
  await page.getByRole('button', { name: '+ Create job' }).click();
  await page.getByLabel('Give this collection a name').fill(jobName);
  await page.locator('#create-dialog').getByRole('button', { name: 'Create job →' }).click();
  await expect(page.locator('#job-title')).toHaveText(jobName);
  await expect(page.locator('#choose-files')).toBeEnabled();
  await page
    .locator('#files')
    .setInputFiles({ name: 'storage-source.txt', mimeType: 'text/plain', buffer: source });
  await expect(page.locator('#upload-label')).toHaveText('1 accepted · 0 failed');
  await page.getByRole('button', { name: 'Commit selected classifications →' }).click();
  await expect(page.locator('#job-state')).toHaveText('committed');
  await expect(page.locator('#job-documents .badge').filter({ hasText: /^indexed$/ })).toHaveCount(
    1,
    { timeout: 20000 },
  );
  await page.getByRole('button', { name: 'Search library', exact: true }).click();
  await page.locator('#search-query').fill(term);
  await page.locator('#filter-job').selectOption({ label: jobName });
  await page.locator('#search-form').getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.locator('#search-results .result-card')).toHaveCount(1);
  await page.locator('#search-results input[type=checkbox]').check();
  await page.getByRole('button', { name: 'Plan move to volume one', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Choose what belongs where.' })).toBeVisible();
  await expect(page.locator('#storage-selected')).toContainText('1 document selected');
  await expect(page.locator('#storage-destination')).toHaveText('/volume1/Theophysics/library');
  await expect(page.locator('.storage-fixed')).toContainText('Your existing PostgreSQL database');
  await expect(page.locator('.storage-fixed')).toContainText('Hister’s full-text search index');
  await expect(page.locator('#storage-page')).toContainText('Automatic deletion: off');
  await page.getByRole('button', { name: 'Preview move to volume one →' }).click();
  await expect(page.locator('#storage-plan-summary')).toContainText('1 file');
  await page.locator('#storage-text').check();
  await page.getByRole('button', { name: 'Preview move to volume one →' }).click();
  await expect(page.locator('#storage-plan-summary')).toContainText('2 files');
  await expect(page.locator('#storage-plan-items')).toContainText(
    'Planned: /volume1/Theophysics/library',
  );
  const downloaded = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download plan', exact: true }).click();
  const download = await downloaded;
  const plan = JSON.parse(await readFile((await download.path())!, 'utf8'));
  expect(plan.status).toBe('preview');
  expect(plan.items).toHaveLength(2);
  expect(plan.kinds).toEqual(['originals', 'extracted_text']);
  const response = await page.request.get(`/v1/documents/${plan.document_ids[0]}/source`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(await response.body()).toEqual(source);
  const detailsResponse = await page.request.get(`/v1/documents/${plan.document_ids[0]}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const details = await detailsResponse.json();
  expect(details.document.canonical_path).toBe(
    plan.items.find((item) => item.kind === 'originals').source,
  );
  expect(details.document.status).toBe('indexed');
  await page.locator('#storage-job').selectOption({ label: jobName });
  await page.locator('#storage-age').selectOption('90');
  await page.getByRole('button', { name: 'Apply filters', exact: true }).click();
  await expect(page.locator('#storage-summary')).toContainText('0 matching research documents');
  await expect(page.locator('#storage-selected')).toContainText('0 documents selected');
  await expect(page.locator('#storage-preview')).toBeDisabled();
  await page.locator('#storage-plan-history').getByRole('button').first().click();
  await expect(page.locator('#storage-plan-summary')).toContainText('2 files');
  await expect(page.locator('#storage-plan')).toContainText('Preview only');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: `.test/theophysics-storage-${test.info().project.name}.png`,
    fullPage: true,
  });
});
