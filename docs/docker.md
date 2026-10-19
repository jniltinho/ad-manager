# Testing with Docker (demo image)

A single image brings up a **Samba 4 domain** with the AD Manager **panel** and **agent** already connected to it: you
can see the system working without preparing a server. It is for **testing and demos**. A production DC is still
installed from the `.deb` ([installation.md](installation.md)), on a machine of its own.

Inside the container, supervisord runs four processes: `samba` (the DC), `redis-server` (the agent's queue),
`ad-manager server` (the panel, as the `ad-manager` user) and `ad-manager agent` (this DC's agent, which also serves
files in `/srv/shares`).

## From Docker Hub

```bash
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 jniltinho/ad-manager
docker logs ad-manager
```

The first start takes about a minute (it provisions the domain). The log shows the URL, the user and the generated
password:

```text
  ┌─ AD Manager ──────────────────────────────────────────────────────────
  │ Panel:    https://localhost:8443
  │ Domain:   example.local (EXAMPLE) · DC dc1.example.local
  │ User:     Administrator   (or EXAMPLE\Administrator)
  │ Password: the generated password
  │           generated now and shown only this once: keep it (change it in the panel under 'my password')
  │ CA:       docker cp <container>:/opt/ad-manager/tls/ca.pem .   (import it in the browser to remove the warning)
  └───────────────────────────────────────────────────────────────────────
```

Open `https://localhost:8443`, accept the certificate warning (or import the CA, below) and sign in as
`Administrator`.

### With your own domain and password

```bash
read -rs -p 'Administrator password: ' AD_ADMIN_PASSWORD; export AD_ADMIN_PASSWORD; echo
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 \
  -e AD_DOMAIN=corp.example.local -e AD_ADMIN_PASSWORD \
  jniltinho/ad-manager
unset AD_ADMIN_PASSWORD
```

`-e AD_ADMIN_PASSWORD` without `=value` passes the variable from your shell: the password does not end up in the
history or on the command line, and the container's processes (samba, panel, agent) do not receive it. Docker,
however, keeps the container's variables: `docker inspect` shows the password. For a test that is enough; to leave no
trace, let the image generate the password or change it in the panel after the first sign-in. The password needs
**8 characters or more**, mixing three of these: lowercase, uppercase, digits and symbols (Samba's default policy);
otherwise the container stops before provisioning and states the rule.

### Variables

| Variable | Default | Purpose |
|---|---|---|
| `AD_DOMAIN` | `example.local` | DNS name of the domain |
| `AD_NETBIOS` | first label of the domain, in uppercase | short name (`EXAMPLE\user`), up to 15 characters |
| `AD_ADMIN_PASSWORD` | generated and shown once in the log | `Administrator` password |
| `AD_PUBLIC_HOST` | `localhost` | name or IP the browser uses: goes into the certificate and the URL shown |
| `AD_PUBLIC_PORT` | `8443` | host port mapped to the container's 8443 (only for the URL shown) |
| `AD_DNS_FORWARDER` | Docker's DNS | where the DC resolves names outside the domain |

The variables apply on the **first** start. After that, the domain, the DC name and the password live in the volume (a
different `AD_DOMAIN` is ignored, with a warning in the log); changing the password is done in the panel ("my
password"). If the first start is interrupted before the domain is finished, the next one starts over from scratch.

`--hostname` is the DC name (`dc1.example.local`): up to 15 characters and different from the NetBIOS name. Without
it, Docker sets a random name, which also works, but changes with every new container.

`--stop-timeout 60` gives supervisord time to stop Samba and the agent in order (Docker's default is 10 s, after which
it kills everything, including a backup or a sync in progress).

`--cap-add SYS_ADMIN` lets Samba write the Windows permissions (`security.*` extended attributes) during provisioning
and on shared folders. Without it, provisioning may fail.

## With Docker Compose

`docker/allinone/compose.yml` brings up the same container with named volumes (the domain survives `down`/`up`):

```bash
docker compose -f docker/allinone/compose.yml up -d             # Docker Hub image
docker compose -f docker/allinone/compose.yml logs ad-manager   # URL, user and the generated password
```

The variables from the table above come from the shell or from `docker/allinone/.env` (outside git), for example:

```bash
cat > docker/allinone/.env <<'ENV'
AD_DOMAIN=corp.example.local
AD_PUBLIC_HOST=192.168.1.50
ENV
```

Without `AD_ADMIN_PASSWORD` the password is generated. To choose it, export the variable in the shell before `up`, as
in the `docker run` above. With `--build`, compose builds this repository instead of pulling the image.
`docker compose -f docker/allinone/compose.yml down -v` removes the container and the volumes (starts from scratch).

## Building

At the repository root:

```bash
make docker-allinone            # jniltinho/ad-manager:<version> and :latest
```

Or directly:

```bash
docker build -f docker/allinone/Dockerfile -t ad-manager-demo .
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 ad-manager-demo
```

The build has three stages: the SPA (Node), the binary (Go, with the SPA embedded) and the runtime (Ubuntu 26.04 with
the distro's Samba, Redis and supervisord). The files are in `docker/allinone/`: `Dockerfile`, `entrypoint.sh` and
`supervisord.conf`.

To publish to Docker Hub, sign in with `docker login` and run `make docker-push`, which pushes the version and
`:latest`. `IMAGE=other/name make docker-push` publishes to another repository.

## Data and restarts

| Volume | What it keeps |
|---|---|
| `/var/lib/samba`, `/etc/samba` | the domain (database, SYSVOL, `smb.conf`) |
| `/opt/ad-manager/data` | the panel database (servers, settings), the agent token, the backups and the syncs (with their credentials) |
| `/opt/ad-manager/tls` | the CA and the certificate |
| `/srv/shares` | the shared folders |

With named volumes, a new container comes back with the same domain, without provisioning again:

```bash
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN -p 8443:8443 \
  -v adm-samba:/var/lib/samba -v adm-etc:/etc/samba -v adm-data:/opt/ad-manager/data \
  -v adm-tls:/opt/ad-manager/tls -v adm-shares:/srv/shares \
  jniltinho/ad-manager
```

To start from scratch: `docker rm -f ad-manager` and `docker volume rm` for the volumes (without `-v`, Docker creates
anonymous volumes, which `docker rm -v` removes along with the container).

## Certificate

On the first start the container creates its **own CA** and a certificate signed by it for the DC name, `localhost`,
`AD_PUBLIC_HOST` and the container's IPs. The same certificate serves Samba's LDAPS (which the panel validates with
this CA), the panel's HTTPS and the agent. For the browser to trust it:

```bash
docker cp ad-manager:/opt/ad-manager/tls/ca.pem ad-manager-ca.pem
```

and import `ad-manager-ca.pem` as a trusted authority (only in a test browser).

## Using it as a real DC (network)

The test above only publishes the panel. To join computers to the domain, clients need to reach the DC ports (53, 88,
135, 139, 389, 445, 464, 636, 3268, 3269 and the high RPC ports). The simplest way is the host network, on a machine
**without** another DNS server or Samba:

```bash
docker run -d --name ad-manager --hostname dc1 --stop-timeout 60 --cap-add SYS_ADMIN --network host \
  -e AD_PUBLIC_HOST=192.168.1.50 jniltinho/ad-manager
```

Clients use the machine's IP as their DNS server. Even so, for production use the `.deb`: the setup takes care of
chrony, DNS, interfaces and backup ([samba-install.md](samba-install.md)).

## Useful commands

```bash
docker logs -f ad-manager                                    # samba, panel and agent
docker exec ad-manager supervisorctl status                  # the four processes
docker exec ad-manager samba-tool user list                  # the domain through samba-tool
docker exec -it ad-manager ad-manager --help                 # the CLI (AD_USER/AD_PASSWORD in the environment)
```

## Limits

- A single DC, no replication, no chrony (the clock is the host's) and no scheduled backup configured.
- The agent runs syncs without systemd confinement (`sync_unconfined = true`), because the container has no systemd.
- Sync with the **machine account** (Kerberos) does not work inside the container (`kinit` for the DC account returns
  `NT_STATUS_NO_LOGON_SERVERS`): use **user and password** in the wizard.
- The container's IP goes into the certificate on the first start; if it changes, the browser keeps using
  `localhost`/`AD_PUBLIC_HOST`, which do not change.
