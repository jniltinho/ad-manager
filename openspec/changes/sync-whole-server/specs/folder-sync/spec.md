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
