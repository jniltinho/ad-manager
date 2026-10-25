## ADDED Requirements

### Requirement: Sync every share of a source server
A sync job of kind `server` SHALL name a source server without a share or a path, SHALL copy only, and each run
SHALL bring every non-administrative share of the source into the folder of the share of the same name published on
this server, one share at a time, recording the summed result and a note per share.

#### Scenario: Whole server copied
- **WHEN** a server job runs against a source with the shares `Financeiro`, `Publico` and `C$`, and this server
  publishes `Financeiro` at `/srv/shares/Financeiro` and `Publico` at `/srv/shares/Users/Publico`
- **THEN** the files of `Financeiro` and `Publico` are copied into those folders, `C$` is not touched, and the run
  shows the summed counts with a note per share

### Requirement: Shares a server job skips
A server job SHALL skip, with the reason, a share not published on this server, a share whose folder is inside
another selected share's folder, and a folder the server cannot sync into.

#### Scenario: Share not published here
- **WHEN** the source has a share `Projetos` that this server does not publish
- **THEN** the run skips it with the note "not published on this server" and copies the others

#### Scenario: Nested share
- **WHEN** this server publishes `Comercial` at `/srv/shares/Comercial` and `Contrato` at
  `/srv/shares/Comercial/Contrato`
- **THEN** `Contrato` is skipped as copied with `Comercial`

#### Scenario: Mirror refused
- **WHEN** a server job is saved with the mirror mode
- **THEN** the agent refuses it: a server job copies only

#### Scenario: The wizard shows the mapping
- **WHEN** the wizard lists a source server with `targets: true`
- **THEN** the answer has, per share, the folder it goes to or the reason it is skipped, before the job is saved

### Requirement: The jobs list names a server job's destination
The Sync page (web and desktop) SHALL show, as the destination of a server job, the share folders of this server
(the agent's share roots), with the mapping by name in its tooltip.

#### Scenario: Destination of a server job
- **WHEN** the Sync page lists a server job on a server whose shares live under `/srv/shares`
- **THEN** its Destination cell shows `/srv/shares` and hovering it says the shares of the same name

### Requirement: Stopping a run asks first
Stopping a queued or running sync run from the web panel or the desktop app SHALL ask for confirmation and SHALL
send nothing until it is given.

#### Scenario: Stop confirmed
- **WHEN** the operator clicks the stop link of a running run and confirms **Stop run**
- **THEN** the cancel request is sent and the list refreshes

#### Scenario: Stop dismissed
- **WHEN** the operator dismisses the confirmation
- **THEN** no request is sent and the run goes on

### Requirement: A share that denies the account is read through its disk's administrative share
When a server job cannot open a source share's root because the share's permission leaves its NTLM account out, the
agent SHALL read that share's folder (from the source's share list) through its disk's administrative share, read
only, and note it in the run; it SHALL never read a whole disk, and a saved job SHALL NOT name an administrative
share.

#### Scenario: Denied share read through E$
- **WHEN** a server job copies a source whose share `ADM` (folder `E:\ADM`) denies the job's account at share level
- **THEN** the run notes that `ADM` is read through `\\source\E$\ADM` and copies its files into the share `ADM` here

#### Scenario: No disk root and no saved administrative share
- **WHEN** a share's folder is a disk's root, or a job is saved with an administrative share
- **THEN** the share fails as before, and the job is refused
