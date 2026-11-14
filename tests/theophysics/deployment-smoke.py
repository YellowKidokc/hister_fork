#!/usr/bin/env python3
"""Exercise a disposable deployed research stack using only the standard library.

Creates one labeled validation job and retains its test document/receipts.
Requires TP_E2E_TOKEN; optional TP_E2E_URL defaults to local port 4444.
Never run against production data without intending to add this test fixture.
"""
import hashlib
import json
import os
import time
import urllib.parse
import urllib.request
import uuid


def main():
    base = os.environ.get("TP_E2E_URL", "http://127.0.0.1:4444").rstrip("/")
    token = os.environ["TP_E2E_TOKEN"]

    def call(path, method="GET", value=None, content_type="application/json"):
        if isinstance(value, (dict, list)):
            value = json.dumps(value).encode()
        req = urllib.request.Request(
            base + path, data=value, method=method,
            headers={"Authorization": "Bearer " + token, "Content-Type": content_type},
        )
        with urllib.request.urlopen(req, timeout=20) as response:
            return response.read(), response.headers

    health, _ = call("/healthz")
    assert json.loads(health)["status"] == "ok"
    term = "deploymentprobe" + uuid.uuid4().hex
    source = ("# Deployment validation fixture\n\n" + term +
              " checks the research library's durable document pipeline.\n").encode()
    data, _ = call("/v1/jobs", "POST", {
        "name": "Deployment validation " + term, "categories": ["physics"],
    })
    job = json.loads(data)

    def upload(name):
        boundary = "TheophysicsValidationBoundary" + uuid.uuid4().hex
        body = ("--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="' +
                name + '"\r\nContent-Type: text/markdown\r\n\r\n').encode()
        body += source + ("\r\n--" + boundary + "--\r\n").encode()
        data, _ = call("/v1/jobs/" + job["id"] + "/files?" +
                       urllib.parse.urlencode({"source": "deployment-validation/" + name}),
                       "POST", body, "multipart/form-data; boundary=" + boundary)
        return json.loads(data)

    first = upload("original.md")
    second = upload("exact-copy.md")
    assert first["document_id"] == second["document_id"] and second["exact_duplicate"]
    document_id = first["document_id"]
    data, _ = call("/v1/jobs/" + job["id"])
    view = json.loads(data)
    assert len(view["documents"]) == 1 and len(view["receipts"]) == 2
    assert view["documents"][0]["sha256"] == hashlib.sha256(source).hexdigest()
    call("/v1/jobs/" + job["id"] + "/commit", "POST", {
        "decisions": [{"document_id": document_id, "categories": ["physics", "information"]}],
    })
    query = "/v1/search?" + urllib.parse.urlencode({"q": term, "category": "physics", "job": job["id"]})
    deadline = time.monotonic() + 40
    while True:
        data, _ = call(query)
        results = json.loads(data)
        if len(results["documents"]) == 1 and results["documents"][0]["id"] == document_id:
            break
        if time.monotonic() >= deadline:
            raise RuntimeError("Committed fixture did not become searchable within 40 seconds")
        time.sleep(0.4)
    markdown, headers = call("/v1/export", "POST", {"ids": [document_id]})
    assert term.encode() in markdown and first["id"].encode() in markdown
    events_data, _ = call("/v1/events")
    events = json.loads(events_data)
    exported = next(e for e in events if e["kind"] == "export.created" and
                    e["details"]["export_id"] == headers["X-Export-ID"])
    assert exported["details"]["sha256"] == hashlib.sha256(markdown).hexdigest()
    downloaded, _ = call("/v1/documents/" + document_id + "/source")
    assert downloaded == source
    print(json.dumps({
        "status": "passed", "job_id": job["id"], "document_id": document_id,
        "query": term, "source_sha256": first["sha256"],
        "checks": ["health", "job", "source preservation", "exact duplicate identity",
                   "classification commit", "Hister indexing", "filtered search",
                   "Markdown export", "export audit hash", "source download"],
    }, indent=2))


if __name__ == "__main__":
    main()
