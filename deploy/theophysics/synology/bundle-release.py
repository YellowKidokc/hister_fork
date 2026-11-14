#!/usr/bin/env python3
"""Build a credential-free DS923+ deployment bundle from this working checkout.

Run from anywhere with Python 3, Git, and Docker Compose. Optional images are
exported from the local daemon; no push, NAS connection, or deployment occurs.
"""
import argparse
import gzip
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import zipfile

IMAGES = ["postgres:17-bookworm", "theophysics-hister:local", "theophysics-control-center:local"]
NEW_PREFIXES = ("cmd/theophysics/", "internal/research/", "deploy/theophysics/", "tests/theophysics/")


def sha256(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def build(repo, output, include_images):
    output.mkdir(parents=True, exist_ok=True)
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=repo).decode().split("\0")
    all_files = subprocess.check_output(["git", "ls-files", "-co", "--exclude-standard", "-z"], cwd=repo).decode().split("\0")
    tracked = set(tracked)
    paths = []
    for name in sorted(set(all_files)):
        if not name or not (name in tracked or name.startswith(NEW_PREFIXES)):
            continue
        path = repo / name
        if path.is_symlink() or not path.is_file():
            raise RuntimeError("Bundle source must be a regular file: " + name)
        if not path.resolve().is_relative_to(repo.resolve()):
            raise RuntimeError("Bundle source escapes repository: " + name)
        if path.name.startswith(".env") and path.name != ".env.example":
            continue
        paths.append(name)
    files = {}
    source = output / "theophysics-source.zip"
    with zipfile.ZipFile(source, "x", zipfile.ZIP_DEFLATED, compresslevel=6) as z:
        for name in paths:
            z.write(repo / name, "theophysics/" + name)
    files[source.name] = sha256(source)
    # Compose resolves syntax here, but keeps settings placeholders intact. No
    # local credential file or runtime environment value is included.
    command = ["docker", "--host=unix:///var/run/docker.sock", "compose", "-f",
               str(repo / "deploy/theophysics/compose.yml"), "config", "--no-interpolate",
               "--no-env-resolution", "--no-path-resolution", "--format", "json"]
    spec = json.loads(subprocess.check_output(command, cwd=repo))
    for service in spec["services"].values():
        service.pop("build", None)
    compose = output / "compose.yaml"
    # JSON is a YAML subset and is accepted by Docker Compose, including DSM.
    with compose.open("x") as f:
        json.dump(spec, f, indent=2)
        f.write("\n")
    files[compose.name] = sha256(compose)
    for origin, dest in [
        ("deploy/theophysics/.env.example", ".env.example"),
        ("deploy/theophysics/synology/inspect-nas.sh", "inspect-nas.sh"),
        ("deploy/theophysics/synology/Inspect-Dlowenas.ps1", "Inspect-Dlowenas.ps1"),
        ("deploy/theophysics/synology/START-HERE.md", "START-HERE.md"),
    ]:
        target = output / dest
        with target.open("xb") as f:
            f.write((repo / origin).read_bytes())
        files[dest] = sha256(target)
    image_ids = {}
    if include_images:
        for image in IMAGES:
            info = json.loads(subprocess.check_output([
                "docker", "--host=unix:///var/run/docker.sock", "image", "inspect", image,
            ]))[0]
            if info["Os"] != "linux" or info["Architecture"] != "amd64":
                raise RuntimeError("DS923+ requires linux/amd64: " + image)
            image_ids[image] = info["Id"]
        target = output / "theophysics-amd64-images.tar.gz"
        with target.open("xb") as f, tempfile.TemporaryFile() as errors:
            proc = subprocess.Popen(["docker", "--host=unix:///var/run/docker.sock", "save", *IMAGES],
                                    stdout=subprocess.PIPE, stderr=errors)
            try:
                with gzip.GzipFile(filename="", mode="wb", fileobj=f, compresslevel=3, mtime=0) as packed:
                    for block in iter(lambda: proc.stdout.read(1 << 20), b""):
                        packed.write(block)
                if proc.wait() != 0:
                    errors.seek(0)
                    raise RuntimeError(errors.read().decode())
            except BaseException:
                proc.kill()
                proc.wait()
                raise
            finally:
                proc.stdout.close()
        files[target.name] = sha256(target)
    manifest = {
        "target": {"model": "Synology DS923+", "hostname": "dlowenas", "architecture": "linux/amd64"},
        "source_head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=repo).decode().strip(),
        "source_includes_uncommitted_working_files": True,
        "source_files": len(paths), "images": image_ids, "files_sha256": files,
        "credentials_included": False, "storage_paths_selected": False, "deployed_to_nas": False,
    }
    with (output / "manifest.json").open("x") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")
    with (output / "SHA256SUMS").open("x") as f:
        for name, checksum in sorted(files.items()):
            f.write(checksum + "  " + name + "\n")
    print(json.dumps({"output": str(output), "files": list(files), "source_files": len(paths),
                      "included_images": include_images, "deployed": False}, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path, help="New/empty output directory; files are never overwritten")
    parser.add_argument("--include-images", action="store_true", help="Export the three built linux/amd64 images (large download)")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    build(root, args.output.resolve(), args.include_images)
