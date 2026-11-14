# Next step: inspect dlowenas before installing

Target: **Synology DS923+, 32 GB RAM, hostname `dlowenas`**. Use Linux AMD64
images. CPU and RAM are suitable for the first research-library slice. The
storage pools, actual mount paths, and permissions have not been inspected yet.

The inspection script is read-only. It reports architecture, DSM version,
capacity/mount types, RAID state, Container Manager/Compose availability, and
existing container names/ports. It does not read passwords, tokens, Docker
configuration, or document contents, and does not install or change anything.
Reports contain local paths/container names; review them before sharing.

## Option A: DSM browser — no SSH needed

1. Download `inspect-nas.sh` from this folder to your computer.
2. In File Station, upload it to a folder you already use for temporary files.
   Use **Properties → Location** to obtain its actual full path. Do not guess
   `/volume1`: the correct volume is one of the things we need to learn.
3. Open **Control Panel → Task Scheduler → Create → Scheduled Task → User-defined
   script**. Name it `Theophysics read-only inspection`, and use your usual DSM
   account. Leave the task **disabled** so it will not run on a schedule.
4. In **Task Settings → User-defined script**, enter the following, replacing the
   path with the real one from File Station:

   ```sh
   /bin/sh '/ACTUAL/PATH/inspect-nas.sh'
   ```

5. Save the disabled task, select it, and choose **Run** once. Inspect its task
   output/result. DSM versions differ; use the Task Scheduler output settings
   if your version requires a destination for logs. Keep that output in a
   private location of your choice.
6. Share the report so we can select separate document, database, and index
   directories. If Docker reports permission denied, the rest of the report
   is still useful. Do not grant broader access just to hide that error.

This task runs only the inspection script; it does not start containers. After
collecting the report, you can remove the disabled inspection task.

## Option B: Windows PowerShell, when SSH is already enabled

Open PowerShell in this folder and run:

```powershell
.\Inspect-Dlowenas.ps1 -SshUser YOUR_DSM_USERNAME
```

The hostname defaults to `dlowenas`. Use `-NasHost` for its known LAN IP or fully
qualified hostname, `-Port` if your SSH port differs, and `-ReportPath` to select
a new output file. Windows OpenSSH handles its own login and host-key prompts.
Verify a new host-key fingerprint using a trusted source before accepting it.
Do not disable host-key checks or put passwords into the script.

The script writes `dlowenas-inspection.txt` in the current Windows folder and
refuses to overwrite an existing report. Windows execution policy and OpenSSH
availability vary; use the DSM option if PowerShell or SSH is unavailable. The
PowerShell helper has been reviewed but has not been executed on Windows in
this Linux cloud environment.

## What happens after inspection

We will select actual storage paths, create new dedicated directories with the
correct service ownership, generate credentials on the NAS, load the tested
images, and start the three-service stack. PostgreSQL and Hister remain private;
only the control center is reachable through your private HTTPS reverse proxy.
No permanent deletion, archive operation, or automatic AI classification is
part of deployment.

The cloud execution environment currently has no route to your home LAN.
Adding the short name `dlowenas` to an Internet allowlist would not create one.
If you already use Tailscale, the environment can instead be connected through
the supported VPN configuration, then access can be checked against your actual
NAS hostname. Do not share Tailscale keys or NAS passwords in chat.

## Deployment bundle contents

The release folder contains `inspect-nas.sh`, the optional Windows helper,
`theophysics-source.zip`, `compose.yaml`, `.env.example`, `manifest.json`, and
`SHA256SUMS`. If built with images, it also contains
`theophysics-amd64-images.tar.gz`. That large archive contains only the three
images, not containers, databases, source documents, credentials, or volumes.
The manifest identifies image IDs and the source snapshot's SHA-256 checksum.
It records that the working source includes the uncommitted first-slice changes.
Nothing has been pushed to a registry or deployed by creating the bundle.

After inspection and storage setup, the offline installation commands on the
NAS will be:

```sh
# Run in the release directory after reviewing the report and selecting paths.
sha256sum -c SHA256SUMS
docker load -i theophysics-amd64-images.tar.gz
# Generate local secrets and fill selected storage paths in a private .env.
# Do not start until the new document/index directories have service ownership.
docker compose --env-file .env -f compose.yaml config --quiet
docker compose --env-file .env -f compose.yaml up -d --no-build
docker compose --env-file .env -f compose.yaml ps
```

`compose.yaml` uses JSON syntax, which is valid YAML. It is the same service
configuration as the source Compose file, with image builds removed. It keeps
all credential and storage variables as placeholders. Loading images does not
start containers. Keep the binding on `127.0.0.1` and use a private HTTPS reverse
proxy for Windows access. The package does not choose `/volume1` or expose the
NAS to the Internet.

For maintainers, generate a new bundle from the repository root after building
the two application images and obtaining PostgreSQL 17:

```sh
python3 deploy/theophysics/synology/bundle-release.py \
  --output /ABSOLUTE/NEW/RELEASE/DIRECTORY --include-images
```

The output files must not already exist; the generator refuses to replace them.
