const $ = (id) => document.getElementById(id);
const el = (tag, text, className) => {
  const node = document.createElement(tag);
  if (text !== undefined) node.textContent = text;
  if (className) node.className = className;
  return node;
};
const insideNogon = location.pathname.endsWith('/research/');
const sessionKey = 'tp-token:' + location.pathname;
let token =
  sessionStorage.getItem(sessionKey) ||
  (insideNogon ? '' : sessionStorage.getItem('tp-token') || '');
let config,
  jobs = [],
  view = null,
  selected = new Set(),
  choices = new Map(),
  exported = new Set();
let busy = false,
  failedFiles = [],
  visible = 100,
  detailID = '',
  page = 'jobs',
  nextKey = '',
  searchPage = 0;
const categoryName = (id) => config?.categories.find((c) => c.id === id)?.name || id;
const date = (value) => new Date(value).toLocaleString();
const bytes = (n) => {
  if (n < 1024) return `${n} B`;
  const unit = Math.min(4, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** unit).toFixed(1)} ${['B', 'KiB', 'MiB', 'GiB', 'TiB'][unit]}`;
};
const counted = (n, label) => `${n} ${label}${n === 1 ? '' : 's'}`;

function notice(text, error = false) {
  $('notice').textContent = text;
  $('notice').className = error ? 'error' : '';
  $('notice').hidden = !text;
}
async function request(path, options = {}, raw = false) {
  const headers = { Authorization: `Bearer ${token}`, ...options.headers };
  if (options.body && !(options.body instanceof FormData))
    headers['Content-Type'] = 'application/json';
  const response = await fetch(new URL(path.slice(1), document.baseURI), { ...options, headers });
  if (!response.ok) {
    let message = `Request failed (${response.status}).`;
    try {
      message = (await response.json()).error || message;
    } catch {}
    if (response.status === 401) {
      $('login').hidden = false;
      $('shell').hidden = true;
    }
    throw new Error(message);
  }
  return raw ? response : response.json();
}
const post = (path, body) => request(path, { method: 'POST', body: JSON.stringify(body) });
function action(fn) {
  return async (...args) => {
    try {
      await fn(...args);
    } catch (error) {
      notice(error.message, true);
    }
  };
}
function setBusy(value) {
  busy = value;
  for (const id of [
    'commit-manual',
    'commit-policy',
    'suggest-ai',
    'choose-files',
    'choose-folder',
    'import-urls',
    'choose-sheet',
  ])
    $(id).disabled =
      value || (id === 'suggest-ai' && !config?.ai_enabled) || view?.job.status !== 'review';
}
function badge(text, kind = '') {
  return el('span', text.replaceAll('_', ' '), `badge ${kind}`);
}
function chips(ids) {
  const box = el('div', undefined, 'chips');
  ids.forEach((id) => box.append(el('span', categoryName(id), 'chip')));
  return box;
}
function categoryPicker(container, current, onChange, disabled = false) {
  container.replaceChildren();
  const axes = [...new Set(config.categories.map((c) => c.axis))];
  for (const axis of axes) {
    const group = el('div', undefined, 'category-group');
    group.append(el('h4', axis.replaceAll('-', ' ')));
    for (const c of config.categories.filter((v) => v.axis === axis)) {
      const label = el('label'),
        input = el('input');
      input.type = 'checkbox';
      input.value = c.id;
      input.checked = current.has(c.id);
      input.disabled = disabled;
      input.addEventListener('change', () => {
        input.checked ? current.add(c.id) : current.delete(c.id);
        onChange?.(current);
      });
      label.append(input, document.createTextNode(c.name));
      group.append(label);
    }
    container.append(group);
  }
}
async function connect() {
  config = await request('/v1/config');
  sessionStorage.setItem(sessionKey, token);
  $('access-token').value = '';
  $('login').hidden = true;
  $('shell').hidden = false;
  $('url-hosts').textContent = config.url_hosts?.length
    ? `Enabled source hosts: ${config.url_hosts.join(', ')}. Redirect hosts must also be enabled.`
    : 'URL fetching is not enabled yet. Set TP_URL_HOSTS on the server to the public source hostnames you want to import.';
  $('suggest-ai').title = config.ai_enabled
    ? 'Send selected document excerpts to your configured model for classification proposals.'
    : 'No AI provider configured. Manual classification and Inbox/Workbench work without AI.';
  for (const c of config.categories) {
    const option = el('option', `${c.name} · ${c.axis}`);
    option.value = c.id;
    $('filter-category').append(option);
    $('storage-category').append(option.cloneNode(true));
  }
  await loadJobs();
}
async function loadJobs() {
  jobs = await request('/v1/jobs');
  renderJobs();
  for (const selector of ['filter-job', 'storage-job']) {
    const current = $(selector).value;
    $(selector).replaceChildren(new Option('All research jobs', ''));
    jobs.forEach((j) => $(selector).append(new Option(j.name, j.id)));
    $(selector).value = current;
  }
}
function renderJobs() {
  const box = $('job-list');
  box.replaceChildren();
  if (!jobs.length) {
    const empty = el('div', undefined, 'empty');
    empty.append(
      el('h3', 'Every discovery starts somewhere.'),
      el(
        'p',
        'Create your first job, then bring in a folder, a few files, or a collection of links.',
      ),
    );
    box.append(empty);
  }
  jobs.forEach((j) => {
    const card = el('button', undefined, 'job-card');
    card.append(
      badge(j.status === 'review' ? 'Ready for sources & review' : 'Committed'),
      el('h3', j.name),
      chips(j.categories),
      el('span', date(j.created_at), 'date'),
    );
    card.addEventListener(
      'click',
      action(() => openJob(j.id)),
    );
    box.append(card);
  });
}
function navigate(to) {
  page = to;
  notice('');
  for (const name of ['jobs', 'library', 'review', 'audit', 'storage']) {
    $(`${name}-page`).hidden = name !== to;
    $(`nav-${name}`).classList.toggle('active', name === to);
  }
  $('breadcrumb').textContent =
    `Workspace / ${to === 'jobs' ? 'Research jobs' : to === 'library' ? 'Search library' : to === 'review' ? 'Classification review' : to === 'storage' ? 'Storage' : 'Activity & receipts'}`;
}
async function openJob(id) {
  setBusy(true);
  view = null;
  selected = new Set();
  choices = new Map();
  visible = 100;
  $('job-list').hidden = true;
  $('job-workspace').hidden = false;
  $('new-job').hidden = true;
  $('upload-status').hidden = true;
  $('upload-errors').replaceChildren();
  failedFiles = [];
  try {
    await refreshJob(id);
  } finally {
    setBusy(false);
    if (view) renderJob();
  }
}
async function refreshJob(id = view?.job.id) {
  if (!id) return;
  view = await request(`/v1/jobs/${id}`);
  for (const d of view.documents) {
    if (!choices.has(d.id)) {
      const initial = view.proposals
        .filter((p) => p.document_id === d.id && (p.source === 'user' || p.source === 'fallback'))
        .map((p) => p.category_id);
      choices.set(d.id, new Set(initial));
      if (!d.extraction_error && view.job.status === 'review') selected.add(d.id);
    }
  }
  renderJob();
}
function renderJob() {
  $('job-title').textContent = view.job.name;
  const exact = view.receipts.filter((r) => r.exact_duplicate).length;
  const failures = view.receipts.filter((r) => r.error).length;
  $('job-summary').textContent =
    `${view.documents.length} canonical documents · ${view.receipts.length} source receipts · ${exact} exact duplicates${failures ? ` · ${failures} failed URL receipts` : ''}`;
  $('job-state').textContent = view.job.status;
  $('intake').hidden = view.job.status !== 'review';
  $('review-help').textContent =
    view.job.status === 'review'
      ? 'Select documents and inspect their labels. Commit your own selections, or explicitly use the confidence policy: ≥95% applies; 80–95% applies with review; below 80% is held. Unknown material goes to Inbox/Workbench. Canon, series, road, and archive require human decisions.'
      : 'This job is committed. Queued documents will appear in search after Hister acknowledges indexing. Unselected sources remain preserved in this job; create another job to import or classify them later.';
  $('select-job-all').disabled = view.job.status !== 'review' || busy;
  $('select-job-all').checked =
    !!view.documents.filter((d) => !d.extraction_error).length &&
    view.documents.filter((d) => !d.extraction_error).every((d) => selected.has(d.id));
  $('job-documents').replaceChildren();
  if (!view.documents.length) {
    const row = el('tr'),
      cell = el('td', 'Your sources will appear here, ready for review.', 'empty');
    cell.colSpan = 5;
    row.append(cell);
    $('job-documents').append(row);
  }
  for (const d of view.documents.slice(0, visible)) {
    const row = el('tr'),
      checkCell = el('td'),
      check = el('input');
    check.type = 'checkbox';
    check.checked = selected.has(d.id);
    check.disabled = busy || view.job.status !== 'review' || !!d.extraction_error;
    check.setAttribute('aria-label', `Select ${d.title}`);
    check.addEventListener('change', () => {
      check.checked ? selected.add(d.id) : selected.delete(d.id);
      renderJob();
    });
    checkCell.append(check);
    const nameCell = el('td'),
      link = el('button', d.title, 'doc-name');
    link.addEventListener(
      'click',
      action(() => showDocument(d.id, true)),
    );
    nameCell.append(link, el('div', `${bytes(d.size)} · SHA ${d.sha256.slice(0, 12)}…`, 'doc-sub'));
    if (d.extraction_error) nameCell.append(el('div', d.extraction_error, 'doc-sub'));
    const catCell = el('td');
    const ids =
      view.job.status === 'review'
        ? [...(choices.get(d.id) || [])]
        : view.memberships.filter((m) => m.document_id === d.id).map((m) => m.category_id);
    catCell.append(chips(ids));
    const proposals = view.proposals.filter((p) => p.document_id === d.id && p.source === 'ai');
    if (proposals.length)
      catCell.append(
        el('div', `${proposals.length} AI proposals · inspect before committing`, 'doc-sub'),
      );
    if (view.job.status === 'review') {
      const edit = el('button', 'Review labels', 'text-button');
      edit.addEventListener(
        'click',
        action(() => showDocument(d.id, true)),
      );
      catCell.append(edit);
    }
    const recCell = el('td'),
      count = view.receipts.filter((r) => r.document_id === d.id).length,
      isExact = view.receipts.some((r) => r.document_id === d.id && r.exact_duplicate),
      probable = view.duplicates.filter((p) => p.document_id === d.id || p.candidate_id === d.id);
    recCell.append(el('div', `${count} source receipt${count === 1 ? '' : 's'}`, 'doc-sub'));
    if (isExact) recCell.append(badge('Exact duplicate · shared identity'));
    if (probable.length) recCell.append(badge(`${probable.length} probable matches`, 'warn'));
    const statusCell = el('td');
    statusCell.append(
      badge(
        d.extraction_error ? 'Needs extraction review' : d.status,
        d.extraction_error || d.status === 'index_failed' ? 'warn' : '',
      ),
    );
    const task = view.tasks.find((t) => t.document_id === d.id);
    if (task?.error)
      statusCell.append(el('div', `Retry ${task.attempts}: ${task.error}`, 'doc-sub'));
    row.append(checkCell, nameCell, catCell, recCell, statusCell);
    $('job-documents').append(row);
  }
  $('more-documents').hidden = visible >= view.documents.length;
  $('selected-count').textContent = `${selected.size} selected for commit`;
  setBusy(busy);
  $('commit-manual').disabled ||= !selected.size;
  $('commit-policy').disabled ||= !selected.size;
  $('suggest-ai').disabled ||= !selected.size;
}
async function uploadFiles(files) {
  if (!view || view.job.status !== 'review' || busy) return;
  const pending = [...files];
  if (!pending.length) return;
  setBusy(true);
  failedFiles = [];
  $('upload-errors').replaceChildren();
  $('upload-status').hidden = false;
  $('retry-uploads').hidden = true;
  $('upload-progress').max = pending.length;
  $('upload-progress').value = 0;
  const jobID = view.job.id;
  try {
    for (let i = 0; i < pending.length; i++) {
      const f = pending[i],
        source = f.relativePath || f.webkitRelativePath || f.name;
      $('upload-label').textContent = `${i + 1} / ${pending.length} · ${source}`;
      try {
        if (f.size > config.max_file_bytes) throw new Error('File exceeds 32 MiB.');
        const form = new FormData();
        form.append('file', f, f.name);
        await request(`/v1/jobs/${jobID}/files?source=${encodeURIComponent(source)}`, {
          method: 'POST',
          body: form,
        });
      } catch (error) {
        failedFiles.push(f);
        $('upload-errors').append(el('li', `${source}: ${error.message}`));
      }
      $('upload-progress').value = i + 1;
    }
    $('upload-label').textContent =
      `${pending.length - failedFiles.length} accepted · ${failedFiles.length} failed`;
    $('retry-uploads').hidden = !failedFiles.length;
    await refreshJob(jobID);
  } finally {
    setBusy(false);
    renderJob();
  }
}
async function droppedFiles(items) {
  const result = [];
  async function visit(entry, prefix = '') {
    if (entry.isFile) {
      const file = await new Promise((resolve, reject) => entry.file(resolve, reject));
      file.relativePath = prefix + file.name;
      result.push(file);
    } else if (entry.isDirectory) {
      const reader = entry.createReader();
      while (true) {
        const batch = await new Promise((resolve, reject) => reader.readEntries(resolve, reject));
        if (!batch.length) break;
        for (const child of batch) await visit(child, `${prefix}${entry.name}/`);
      }
    }
  }
  for (const item of items) {
    const entry = item.webkitGetAsEntry?.();
    if (entry) await visit(entry);
    else {
      const file = item.getAsFile();
      if (file) result.push(file);
    }
  }
  return result;
}
async function importURLs(urls) {
  if (busy || !view) return;
  const list = [...new Set(urls.map((u) => u.trim()).filter(Boolean))];
  if (!list.length) return;
  setBusy(true);
  $('upload-errors').replaceChildren();
  $('upload-status').hidden = false;
  $('upload-progress').max = list.length;
  let accepted = 0;
  try {
    for (let i = 0; i < list.length; i++) {
      $('upload-label').textContent = `Importing URL ${i + 1} / ${list.length}`;
      try {
        await post(`/v1/jobs/${view.job.id}/url`, { url: list[i] });
        accepted++;
      } catch (error) {
        $('upload-errors').append(el('li', `${list[i]}: ${error.message}`));
      }
      $('upload-progress').value = i + 1;
    }
    $('upload-label').textContent =
      `${accepted} URLs imported · ${list.length - accepted} failed (receipts retained)`;
    await refreshJob();
  } finally {
    setBusy(false);
    renderJob();
  }
}
async function showDocument(id, editable = false) {
  const data = await request(`/v1/documents/${id}`);
  detailID = id;
  $('detail-title').textContent = data.document.title;
  $('detail-meta').replaceChildren(
    el('div', `Identity: ${id}`),
    el('div', `SHA-256: ${data.document.sha256}`),
    el('div', `Canonical source: ${data.document.canonical_path}`),
    el('div', `${bytes(data.document.size)} · ${data.document.status}`),
  );
  const canEdit = editable && view?.job.status === 'review' && !busy;
  const current = canEdit ? choices.get(id) : new Set(data.memberships.map((m) => m.category_id));
  categoryPicker($('detail-categories'), current || new Set(), () => renderJob(), !canEdit);
  $('detail-proposals').replaceChildren();
  const proposals = (editable ? view?.proposals || [] : []).filter(
    (p) => p.document_id === id && p.source === 'ai',
  );
  if (proposals.length) {
    $('detail-proposals').append(el('h3', 'AI proposals · independent suggestions'));
    for (const p of proposals) {
      const row = el('div', undefined, 'receipt');
      row.append(
        el('strong', `${categoryName(p.category_id)} · ${(p.confidence * 100).toFixed(0)}%`),
        el('p', p.reason),
      );
      if (canEdit) {
        const accept = el('button', 'Add to my selections', 'text-button');
        accept.addEventListener('click', () => {
          choices.get(id).add(p.category_id);
          categoryPicker($('detail-categories'), choices.get(id), () => renderJob());
          renderJob();
          accept.disabled = true;
        });
        row.append(accept);
      }
      $('detail-proposals').append(row);
    }
  }
  const dupes = (editable ? view?.duplicates || [] : []).filter(
    (p) => p.document_id === id || p.candidate_id === id,
  );
  $('detail-duplicates').replaceChildren();
  if (dupes.length) {
    $('detail-duplicates').append(el('h3', 'Probable duplicates · never merged automatically'));
    dupes.forEach((p) =>
      $('detail-duplicates').append(
        el(
          'div',
          `${p.kind} · ${(p.similarity * 100).toFixed(0)}% fingerprint overlap · candidate ${p.document_id === id ? p.candidate_id : p.document_id}`,
          'receipt',
        ),
      ),
    );
  }
  $('detail-receipts').replaceChildren(el('h3', 'Source receipts'));
  for (const r of data.receipts)
    $('detail-receipts').append(
      el(
        'div',
        `${r.source}\n${date(r.created_at)} · job ${r.job_id}\nReceipt ${r.id}${r.exact_duplicate ? ' · Exact duplicate' : ''}`,
        'receipt',
      ),
    );
  const reviewMemberships = data.memberships.filter((m) => m.review_required);
  if (reviewMemberships.length)
    $('detail-meta').append(
      el(
        'div',
        `Needs classification review: ${reviewMemberships.map((m) => categoryName(m.category_id)).join(', ')}`,
      ),
    );
  $('detail-text').textContent =
    data.text.slice(0, 150000) || data.document.extraction_error || 'No text extracted.';
  if (data.text.length > 150000)
    $('detail-text').append(
      document.createTextNode(
        '\n\n[Preview truncated. Markdown export contains the complete extracted text.]',
      ),
    );
  $('detail-dialog').showModal();
}
async function commit(policy = false) {
  if (!selected.size || busy) return;
  setBusy(true);
  try {
    const body = policy
      ? { policy_ids: [...selected] }
      : {
          decisions: [...selected].map((id) => ({
            document_id: id,
            categories: [...choices.get(id)],
          })),
        };
    await post(`/v1/jobs/${view.job.id}/commit`, body);
    notice(
      'Job committed. Sources and metadata are preserved; Hister indexing will continue in the background.',
    );
    await refreshJob();
    await loadJobs();
  } finally {
    setBusy(false);
    renderJob();
  }
}
async function search(reset = true) {
  if (reset) {
    nextKey = '';
    searchPage = 0;
    exported.clear();
  }
  const params = new URLSearchParams({
    q: $('search-query').value,
    category: $('filter-category').value,
    job: $('filter-job').value,
    page_key: nextKey,
    page: String(searchPage),
  });
  const result = await request(`/v1/search?${params}`);
  nextKey = result.next_page_key;
  $('search-results').replaceChildren();
  $('search-summary').textContent = $('search-query').value
    ? `${result.documents.length} matching documents on this page · Hister has ${result.upstream_total} text matches before metadata filters`
    : `${result.total} indexed documents in this view`;
  if (!result.documents.length)
    $('search-results').append(
      el(
        'div',
        result.has_more
          ? 'No documents in this filtered page. Continue to the next results.'
          : 'No indexed documents found. Commit a research job or try a different query.',
        'empty',
      ),
    );
  for (const d of result.documents) {
    const card = el('article', undefined, 'result-card'),
      check = el('input');
    check.type = 'checkbox';
    check.checked = exported.has(d.id);
    check.setAttribute('aria-label', `Select ${d.title} for export`);
    check.addEventListener('change', () => {
      check.checked ? exported.add(d.id) : exported.delete(d.id);
      renderExport();
    });
    const body = el('div', undefined, 'result-body'),
      title = el('button', d.title, 'doc-name');
    title.addEventListener(
      'click',
      action(() => showDocument(d.id)),
    );
    body.append(
      title,
      chips(result.memberships.filter((m) => m.document_id === d.id).map((m) => m.category_id)),
      el('div', `${d.name} · ${bytes(d.size)}`, 'doc-sub'),
      el('div', `SHA-256 ${d.sha256}`, 'hash'),
    );
    card.append(check, body, badge('Indexed'));
    $('search-results').append(card);
  }
  $('next-results').hidden = !result.has_more;
  renderExport();
}
function renderExport() {
  $('export').disabled = !exported.size || exported.size > 100;
  $('plan-selected-storage').disabled = !exported.size || exported.size > 100;
  $('export-count').textContent = `${exported.size} selected · up to 100 per export`;
}
async function download(response, fallback) {
  const url = URL.createObjectURL(await response.blob()),
    link = el('a');
  link.href = url;
  const disposition = response.headers.get('Content-Disposition') || '';
  const encoded = disposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
  const regular = disposition.match(/filename=(?:"([^"]+)"|([^;]+))/i);
  link.download = encoded
    ? decodeURIComponent(encoded)
    : (regular?.[1] || regular?.[2] || fallback).trim();
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function events() {
  const list = await request('/v1/events');
  $('events').replaceChildren();
  if (!list.length)
    $('events').append(el('div', 'Actions and receipts will appear here as you work.', 'empty'));
  list.forEach((e) => {
    const row = el('article', undefined, 'event'),
      content = el('div');
    const time = el('time', date(e.created_at));
    time.dateTime = e.created_at;
    content.append(
      el('strong', e.kind),
      el(
        'pre',
        JSON.stringify(
          { job: e.job_id || undefined, document: e.document_id || undefined, ...e.details },
          null,
          2,
        ),
      ),
    );
    row.append(time, content);
    $('events').append(row);
  });
}

$('login-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  token = $('access-token').value;
  try {
    await connect();
  } catch (error) {
    $('login-error').textContent = error.message;
  }
});
$('disconnect').addEventListener('click', () => {
  sessionStorage.removeItem(sessionKey);
  if (!insideNogon) sessionStorage.removeItem('tp-token');
  location.reload();
});
$('nav-jobs').addEventListener(
  'click',
  action(async () => {
    navigate('jobs');
    await loadJobs();
  }),
);
$('nav-library').addEventListener(
  'click',
  action(async () => {
    navigate('library');
    await loadJobs();
    await search();
  }),
);
$('nav-audit').addEventListener(
  'click',
  action(async () => {
    navigate('audit');
    await events();
  }),
);
let initialCategories = new Set();
$('new-job').addEventListener('click', () => {
  initialCategories = new Set();
  categoryPicker($('initial-categories'), initialCategories);
  $('job-name').value = '';
  $('create-dialog').showModal();
});
$('create-form').addEventListener(
  'submit',
  action(async (event) => {
    event.preventDefault();
    const job = await post('/v1/jobs', {
      name: $('job-name').value,
      categories: [...initialCategories],
    });
    $('create-dialog').close();
    await loadJobs();
    await openJob(job.id);
  }),
);
document
  .querySelectorAll('.close-dialog')
  .forEach((b) => b.addEventListener('click', () => b.closest('dialog').close()));
$('back-jobs').addEventListener(
  'click',
  action(async () => {
    if (busy) return;
    view = null;
    $('job-workspace').hidden = true;
    $('job-list').hidden = false;
    $('new-job').hidden = false;
    await loadJobs();
  }),
);
$('refresh-job').addEventListener(
  'click',
  action(() => refreshJob()),
);
$('more-documents').addEventListener('click', () => {
  visible += 100;
  renderJob();
});
$('choose-files').addEventListener('click', (e) => {
  e.stopPropagation();
  $('files').click();
});
$('choose-folder').addEventListener('click', (e) => {
  e.stopPropagation();
  $('folder').click();
});
for (const id of ['files', 'folder'])
  $(id).addEventListener(
    'change',
    action(async (e) => {
      await uploadFiles(e.target.files);
      e.target.value = '';
    }),
  );
$('retry-uploads').addEventListener(
  'click',
  action(() => uploadFiles(failedFiles)),
);
$('dropzone').addEventListener('click', (e) => {
  if (e.target.closest('button') || busy) return;
  $('files').click();
});
$('dropzone').addEventListener('keydown', (e) => {
  if (e.target === $('dropzone') && ['Enter', ' '].includes(e.key)) {
    e.preventDefault();
    if (!busy) $('files').click();
  }
});
$('dropzone').addEventListener('dragover', (e) => {
  e.preventDefault();
  $('dropzone').classList.add('dragover');
});
$('dropzone').addEventListener('dragleave', () => $('dropzone').classList.remove('dragover'));
$('dropzone').addEventListener(
  'drop',
  action(async (e) => {
    e.preventDefault();
    $('dropzone').classList.remove('dragover');
    if (busy) return;
    await uploadFiles(await droppedFiles([...e.dataTransfer.items]));
  }),
);
$('import-urls').addEventListener(
  'click',
  action(() => importURLs($('source-urls').value.split('\n'))),
);
$('choose-sheet').addEventListener('click', () => $('sheet').click());
$('sheet').addEventListener(
  'change',
  action(async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    const data = new FormData();
    data.append('file', file);
    const result = await request(`/v1/jobs/${view.job.id}/links`, { method: 'POST', body: data });
    $('source-urls').value = result.urls.join('\n');
    e.target.value = '';
    await refreshJob();
    notice(
      `Preserved spreadsheet and loaded ${result.urls.length} links. Review the list, then select Import URLs.`,
    );
  }),
);
$('select-job-all').addEventListener('change', (e) => {
  selected = new Set(
    e.target.checked ? view.documents.filter((d) => !d.extraction_error).map((d) => d.id) : [],
  );
  renderJob();
});
$('suggest-ai').addEventListener(
  'click',
  action(async () => {
    if (busy) return;
    setBusy(true);
    let complete = 0;
    try {
      for (const id of selected) {
        await post(`/v1/jobs/${view.job.id}/classify`, { document_id: id });
        complete++;
        notice(`AI proposals prepared for ${complete} / ${selected.size} selected documents.`);
      }
    } finally {
      await refreshJob();
      setBusy(false);
      renderJob();
    }
  }),
);
$('commit-manual').addEventListener(
  'click',
  action(() => commit()),
);
$('commit-policy').addEventListener(
  'click',
  action(() => commit(true)),
);
$('search-form').addEventListener(
  'submit',
  action(async (e) => {
    e.preventDefault();
    await search();
  }),
);
$('next-results').addEventListener(
  'click',
  action(async () => {
    if (!$('search-query').value) searchPage++;
    await search(false);
  }),
);
$('export').addEventListener(
  'click',
  action(async () => {
    const response = await request(
      '/v1/export',
      { method: 'POST', body: JSON.stringify({ ids: [...exported] }) },
      true,
    );
    await download(response, 'theophysics-research.md');
    notice('Markdown exported. The export hash and selected identities are recorded in activity.');
  }),
);
$('download-source').addEventListener(
  'click',
  action(async () => {
    await download(await request(`/v1/documents/${detailID}/source`, {}, true), 'source');
  }),
);
$('refresh-events').addEventListener('click', action(events));
setInterval(async () => {
  if (
    page === 'jobs' &&
    view &&
    !busy &&
    view.documents.some((d) => ['queued', 'index_failed'].includes(d.status)) &&
    !$('detail-dialog').open
  ) {
    try {
      await refreshJob();
    } catch {}
  }
}, 3000);
if (insideNogon) $('legacy-jobs').hidden = false;
if (token) {
  connect().catch((error) => {
    $('login-error').textContent = error.message;
  });
}

async function reviewQueue() {
  const data = await request('/v1/review');
  const box = $('review-queue');
  box.replaceChildren();
  if (!data.proposals.length)
    box.append(el('div', 'No classifications are waiting for review.', 'empty'));
  for (const proposal of data.proposals) {
    const doc = data.documents.find((d) => d.id === proposal.document_id);
    const row = el('article', undefined, 'result-card'),
      body = el('div', undefined, 'result-body');
    const title = el('button', doc?.title || proposal.document_id, 'doc-name');
    title.addEventListener(
      'click',
      action(() => showDocument(proposal.document_id)),
    );
    body.append(
      title,
      chips([proposal.category_id]),
      el(
        'p',
        `${(proposal.confidence * 100).toFixed(0)}% confidence · ${proposal.status === 'held' ? 'Not applied' : 'Applied provisionally; review required'}`,
      ),
      el('p', proposal.reason),
    );
    const buttons = el('div', undefined, 'button-row');
    for (const accept of [true, false]) {
      const button = el(
        'button',
        accept ? 'Accept category' : 'Reject',
        accept ? 'primary' : 'secondary',
      );
      button.addEventListener(
        'click',
        action(async () => {
          await post('/v1/review/' + proposal.id, { accept });
          await reviewQueue();
        }),
      );
      buttons.append(button);
    }
    body.append(buttons);
    row.append(body);
    box.append(row);
  }
}
$('nav-review').addEventListener(
  'click',
  action(async () => {
    navigate('review');
    await reviewQueue();
  }),
);
$('refresh-review').addEventListener('click', action(reviewQueue));

// Storage previews intentionally have no filesystem execution endpoint.
let storageData = null,
  storageSelected = new Set(),
  storagePage = 0,
  storagePlan = null,
  storageBusy = false;
function storageKinds() {
  return [
    $('storage-originals').checked ? 'originals' : '',
    $('storage-text').checked ? 'extracted_text' : '',
  ].filter(Boolean);
}
function resetStoragePreview() {
  storagePlan = null;
  $('storage-plan').hidden = true;
}
function storageSelectionChanged() {
  $('storage-selected').textContent =
    `${counted(storageSelected.size, 'document')} selected · up to 100 per preview`;
  $('storage-preview').disabled =
    storageBusy || !storageSelected.size || storageSelected.size > 100 || !storageKinds().length;
  $('storage-select-page').checked =
    !!storageData?.documents.length &&
    storageData.documents.every((d) => storageSelected.has(d.id));
}
async function loadStorage(reset = true) {
  if (reset) {
    storagePage = 0;
    storageSelected.clear();
    resetStoragePreview();
  }
  const params = new URLSearchParams({
    job: $('storage-job').value,
    category: $('storage-category').value,
    older_than_days: $('storage-age').value,
    page: String(storagePage),
  });
  storageData = await request(`/v1/storage?${params}`);
  $('storage-destination').textContent = storageData.destination;
  $('storage-summary').textContent =
    `${storageData.documents.length} shown · ${storageData.total} matching research documents`;
  $('storage-documents').replaceChildren();
  if (!storageData.documents.length) {
    const row = el('tr'),
      cell = el('td', 'No research documents match these filters.', 'empty');
    cell.colSpan = 5;
    row.append(cell);
    $('storage-documents').append(row);
  }
  for (const doc of storageData.documents) {
    const row = el('tr'),
      checkboxCell = el('td'),
      checkbox = el('input');
    checkbox.type = 'checkbox';
    checkbox.checked = storageSelected.has(doc.id);
    checkbox.setAttribute('aria-label', `Select ${doc.title} for storage`);
    checkbox.addEventListener('change', () => {
      checkbox.checked ? storageSelected.add(doc.id) : storageSelected.delete(doc.id);
      resetStoragePreview();
      storageSelectionChanged();
    });
    checkboxCell.append(checkbox);
    const nameCell = el('td'),
      title = el('button', doc.title, 'doc-name');
    title.addEventListener(
      'click',
      action(() => showDocument(doc.id)),
    );
    nameCell.append(title, el('div', `SHA ${doc.sha256.slice(0, 12)}… · ${doc.status}`, 'doc-sub'));
    const categories = el('td');
    categories.append(
      chips(
        storageData.memberships.filter((m) => m.document_id === doc.id).map((m) => m.category_id),
      ),
    );
    row.append(
      checkboxCell,
      nameCell,
      el('td', bytes(doc.size)),
      categories,
      el('td', date(doc.created_at)),
    );
    $('storage-documents').append(row);
  }
  $('storage-next').hidden = !storageData.has_more;
  storageSelectionChanged();
  await loadStorageHistory();
}
function renderStoragePlan(plan) {
  storagePlan = plan;
  $('storage-plan').hidden = false;
  $('storage-plan-summary').textContent =
    `${counted(plan.document_ids.length, 'document')} · ${counted(plan.items.length, 'file')} · ${bytes(plan.total_bytes)} proposed · saved ${date(plan.created_at)}`;
  $('storage-plan-items').replaceChildren();
  for (const item of plan.items) {
    const row = el('article', undefined, 'storage-plan-item');
    row.append(
      el('strong', item.name),
      badge(item.kind === 'originals' ? 'Original source' : 'Extracted text'),
    );
    row.append(
      el('div', `Current: ${item.source}`),
      el('div', `Planned: ${item.destination}`),
      el(
        'small',
        `${bytes(item.bytes)}${item.expected_sha256 ? ` · Expected source SHA-256: ${item.expected_sha256}` : ''}`,
      ),
    );
    $('storage-plan-items').append(row);
  }
}
async function loadStorageHistory() {
  const plans = await request('/v1/storage/plans');
  const box = $('storage-plan-history');
  box.replaceChildren();
  if (!plans.length) box.append(el('p', 'No move previews saved yet.'));
  for (const plan of plans) {
    const button = el(
      'button',
      `${date(plan.created_at)} · ${counted(plan.items.length, 'file')} · ${bytes(plan.total_bytes)} · preview only`,
      'text-button',
    );
    button.addEventListener('click', () => renderStoragePlan(plan));
    box.append(button);
  }
}
$('nav-storage').addEventListener(
  'click',
  action(async () => {
    navigate('storage');
    await loadJobs();
    await loadStorage();
  }),
);
$('storage-filter-form').addEventListener(
  'submit',
  action(async (event) => {
    event.preventDefault();
    await loadStorage();
  }),
);
$('storage-next').addEventListener(
  'click',
  action(async () => {
    storagePage++;
    await loadStorage(false);
  }),
);
$('storage-clear').addEventListener(
  'click',
  action(async () => {
    storageSelected.clear();
    resetStoragePreview();
    await loadStorage(false);
  }),
);
$('storage-select-page').addEventListener(
  'change',
  action(async (event) => {
    for (const doc of storageData.documents)
      event.target.checked ? storageSelected.add(doc.id) : storageSelected.delete(doc.id);
    resetStoragePreview();
    await loadStorage(false);
  }),
);
for (const name of ['storage-originals', 'storage-text'])
  $(name).addEventListener('change', () => {
    resetStoragePreview();
    storageSelectionChanged();
  });
$('storage-preview').addEventListener(
  'click',
  action(async () => {
    if (storageBusy) return;
    storageBusy = true;
    storageSelectionChanged();
    try {
      const plan = await post('/v1/storage/preview', {
        document_ids: [...storageSelected],
        kinds: storageKinds(),
      });
      renderStoragePlan(plan);
      await loadStorageHistory();
      notice('Move preview saved. No files were moved or deleted.');
    } finally {
      storageBusy = false;
      storageSelectionChanged();
    }
  }),
);
$('storage-download-plan').addEventListener('click', async () => {
  if (!storagePlan) return;
  const response = new Response(JSON.stringify(storagePlan, null, 2), {
    headers: { 'Content-Type': 'application/json' },
  });
  await download(response, `theophysics-move-preview-${storagePlan.id}.json`);
});
$('plan-selected-storage').addEventListener(
  'click',
  action(async () => {
    const ids = [...exported];
    navigate('storage');
    await loadJobs();
    $('storage-job').value = '';
    $('storage-category').value = '';
    $('storage-age').value = '0';
    await loadStorage();
    storageSelected = new Set(ids);
    await loadStorage(false);
    notice(
      'Selected library documents are ready for a storage preview. Choose the data types, then preview the move.',
    );
  }),
);
