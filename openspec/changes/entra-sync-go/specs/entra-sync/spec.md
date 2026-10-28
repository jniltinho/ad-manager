## ADDED Requirements

### Requirement: Entra sync without Python
`ad-manager entra` SHALL sync users, groups, optional devices and NT password hashes from an Active Directory
(Samba 4 or Windows) to Microsoft Entra ID with no Python interpreter or Python package installed. It SHALL read
the directory over LDAPS and the NT hashes over a sealed, read-only DRSUAPI `GetNCChanges` (`EXOP_REPL_OBJ`), on
a domain controller, a member host and a Windows DC alike, and SHALL accept a decrypted hash only after its CRC
checks.

#### Scenario: Ubuntu 26.04 host without Python packages
- **WHEN** `ad-manager entra sync --dry-run` runs on Ubuntu 26.04 where `python3-samba`, `python3-ldb`,
  `python3-msal` and `python3-cryptography` are not installed
- **THEN** it reads the domain, prints the objects it would send and exits 0

#### Scenario: Hash read is read-only and sealed
- **WHEN** the run reads the NT hash of `alice` from the Docker Samba DC
- **THEN** the hash equals the one `samba-tool user getpassword alice --attributes=unicodePwd` returns, the
  DRSUAPI connection used the privacy (sealing) level, and only `DRSBind`, `DRSGetNCChanges` and `DRSUnbind` were
  called

#### Scenario: Windows Server 2022 DC
- **WHEN** the same read runs against the Windows Server 2022 DC of the Vagrant lab
- **THEN** the hash equals the one the lab's reference tool reports for the same user

#### Scenario: Corrupt or wrongly keyed secret
- **WHEN** the encrypted `unicodePwd` is truncated, has a flipped byte, or is decrypted with a wrong session key
- **THEN** the read fails for that user with an error, no hash is produced and nothing panics

#### Scenario: Inner DRS error
- **WHEN** `DRSGetNCChanges` returns success but its extended return is not `EXOP_ERR_SUCCESS`
- **THEN** the read fails with the extended error and no hash is produced

#### Scenario: Sync account without replication rights
- **WHEN** the sync account lacks *Replicating Directory Changes All*
- **THEN** the run fails before sending anything, with an error naming the missing right, and no hash is sent

#### Scenario: AD password only from the environment
- **WHEN** `config.toml` contains `[entra] ad_password = "…"`
- **THEN** the configuration is refused with "the AD password is read only from ENTRA_AD_PASSWORD or
  ad_password_file", and `ad_password_file` with a mode other than `0600` is refused the same way

### Requirement: Objects computed like AzureADConnect_Samba4
The engine SHALL compute, for each user, group and device, the same attribute set AzureADConnect_Samba4 computes
(source anchor, `accountEnabled`, login from `alternate_login_id_attr`, manager as an anchor, group members as
anchors, `SecurityEnabled` from `groupType`, device id and trust type), apart from the documented differences in
the design.

#### Scenario: Parity with the Python tool
- **WHEN** the parity script runs AzureADConnect_Samba4 (member mode, every Microsoft call and identity step
  stubbed, offline container) and `entrasync sync --dry-run --dump` against the same Docker Samba fixture
- **THEN** both produce the same set of objects field for field, except the named differences: a passwordless
  user (sent without hash instead of skipped or sent with `X`s), values the Python's binary writer alters, and
  the deletion scope

#### Scenario: Expired account is disabled
- **WHEN** an enabled user's `accountExpires` is in the past
- **THEN** it is sent with `accountEnabled = false`

#### Scenario: Built-in objects are never synced
- **WHEN** the fixture has `Administrator` (RID 500), `Domain Admins` and `BUILTIN\Users`
- **THEN** none of them is in the computed set

#### Scenario: Consistency GUID written once
- **WHEN** `write_ms_ds_consistency_guid_if_empty` is on, the run is not a dry run and a user has no
  `ms-DS-ConsistencyGuid`
- **THEN** the selected anchor's raw bytes (the user's `objectGUID` with the default `source_anchor_attr`) are
  written to `ms-DS-ConsistencyGuid` over LDAPS, audited, and the next run computes the same anchor string without
  writing again

#### Scenario: Anchor encoding as in the Python
- **WHEN** an object's anchor bytes are valid UTF-8, and another's are not
- **THEN** the first anchor is that UTF-8 text and the second is the base64 of the bytes, as
  `return_source_anchor` gives

#### Scenario: Anchor attribute changed
- **WHEN** the state database is not empty and `source_anchor_attr` or `use_ms_ds_consistency_guid_if_exist`
  differs from the policy it was built with, or the tenant or domain SID differ
- **THEN** the run refuses to start and nothing is deleted or sent

#### Scenario: Duplicate mail warning
- **WHEN** two users share `chewie@example.test`, one in `mail` and the other in `proxyAddresses`
- **THEN** the log has a `warning_duplicate_mail_value` line naming both DNs and attributes, and both are still
  sent

### Requirement: Microsoft AD Sync protocol in Go
The library SHALL talk to the Azure AD Sync service in `application/soap+msbin1` through its own .NET Binary XML
codec, send every value verbatim and XML-escaped, delete with `ProvisionAzureADSyncObjects` and `SyncOperation =
Delete`, and build the password hash in the `v1;PPH1_MD4,…` format with a salt from `crypto/rand`.

#### Scenario: Codec interoperates with python-wcfbin
- **WHEN** each request envelope is encoded by the Go codec and decoded by python-wcfbin, and the reverse
- **THEN** both sides give the same canonical XML, and the python-wcfbin doctest vectors decode the same in Go
  except python-wcfbin's reader defects (TimeSpan scale, DateTime time zone and precision), where Go gives the
  [MC-NBFX] value

#### Scenario: Lists and arrays
- **WHEN** the reader gets the [MC-NBFX] examples of a list in an attribute value and of an Int16 array
- **THEN** it returns `<doc a="123 hello true"></doc>` and one element per array item

#### Scenario: Values kept verbatim
- **WHEN** a user's `department` is `True`, its `title` is `Sales  `, its `telephoneNumber` is `0612345678` and
  its `description` is a line break and a space
- **THEN** the decoded envelope carries exactly those strings

#### Scenario: Hostile binary XML
- **WHEN** the reader gets a truncated record, a length past the input, nesting deeper than 256, more than
  16 MiB, 16 MiB of dictionary-string records that would expand past 64 MiB, two root elements, invalid UTF-8, a
  lone UTF-16 surrogate or a control character
- **THEN** it returns an error without panicking, without replacing characters and without growing its output
  past 64 MiB

#### Scenario: Special characters in attributes
- **WHEN** a user's `displayName` is `R&D <Lab>` and its `department` is `</c:Value><c:Key>x`
- **THEN** the envelope carries both values as text and no extra property

#### Scenario: AAD hash vector
- **WHEN** the hash of NT hash `8846F7EAEE8FB117AD06BDD830B7586C` is built with salt `b477124de54c2030378f`
- **THEN** the result equals the one AADInternals_python's `create_aadhash` gives for the same salt

### Requirement: Entra identity adopted, never recreated
`entra` SHALL use the Python tool's `entra_aba_dir` layout (state, PFX, passphrase) and its checks, SHALL create
the certificate application only through `entra init` from an interactive terminal, and SHALL refuse to run
rather than create a second application when local identity material is missing or inconsistent.

#### Scenario: Existing Python install
- **WHEN** `entra status` runs with an `entra_aba_dir` the Python tool made ready
- **THEN** it prints the same application id and certificate expiry and the directory is unchanged

#### Scenario: Missing PFX
- **WHEN** `state.json` says `ready` but `current.pfx` is missing
- **THEN** `entra sync` and `entra init` fail with "restore the backup; do not recreate the application" and no
  request reaches Microsoft Graph

#### Scenario: Unattended bootstrap
- **WHEN** `entra sync --service` starts without a ready identity
- **THEN** it exits with "initial Entra setup needs an interactive terminal (`ad-manager entra init`)"

#### Scenario: Insecure identity directory
- **WHEN** `entra_aba_dir` is mode `0755` or `.pfx-passphrase` is a symlink
- **THEN** the run is refused before reading the PFX

#### Scenario: Certificate rotation
- **WHEN** a run that is not a dry run starts and the active certificate has 20 days left
- **THEN** it registers a new certificate with `addKey`, switches to it after Microsoft issues a token with it,
  and keeps the old one listed as retired; if `addKey` fails, it logs a warning and syncs with the current one

#### Scenario: Rotation in dry run
- **WHEN** a dry run starts with 20 days left and a retired key past its grace period
- **THEN** it prints that it would rotate and clean up, and neither Graph nor `entra_aba_dir` changes

#### Scenario: Prerequisites missing
- **WHEN** `entra sync` runs on a tenant where DirSync is off or, with hash sync on, the password hash sync feature
  is off
- **THEN** it exits with "run `ad-manager entra init`" before sending anything; `entra init` turns them on and
  keeps every other sync feature bit

### Requirement: Deletions guarded
By default the engine SHALL delete from Entra only objects in its own state for this tenant and domain that are no
longer in the AD read and that Graph still shows with that immutable id and sync enabled; the tenant-wide scope
SHALL be opt-in. It SHALL stop all deletions of a run when they exceed `max_deletes` or when the AD read returned no
user while the cloud or state has some, and SHALL remove a state row only after Microsoft accepted the deletion.

#### Scenario: Broken base DN
- **WHEN** `basedn_user` points at an empty OU and the state has 120 users
- **THEN** no object is deleted, the log has `delete_guard`, and the command exits non-zero

#### Scenario: Above the threshold
- **WHEN** 600 users would be deleted with the default `max_deletes = 500`
- **THEN** none is deleted unless the run has `--allow-deletes 600`

#### Scenario: Object of another connector
- **WHEN** the tenant has 100 sync-enabled users with immutable ids that are not in this tool's state
- **THEN** with the default `delete_scope = "state"` none of them is deleted

#### Scenario: Group deletion needs one typed match
- **WHEN** a group in the state is gone from AD and the read-back has no group entry with its anchor, two of them,
  or only a user entry with it
- **THEN** it is not deleted and the log has `group_delete_skipped`

#### Scenario: Group deletion without the read-back call
- **WHEN** `use_get_syncobjects = false` and a group in the state is gone from AD
- **THEN** it is not deleted, the log has `group_delete_skipped`, and its state row stays

#### Scenario: User turned cloud-only
- **WHEN** a user in the state is gone from AD but Graph shows it with `onPremisesSyncEnabled = false` or without
  an immutable id
- **THEN** it is not deleted and its state row is dropped (in dry run the row stays and the drop is only
  reported); this is the one state change made without a deletion

#### Scenario: Rejected deletion
- **WHEN** Microsoft returns a failure result for a deletion
- **THEN** the state row stays and the deletion is retried next run

### Requirement: Clean stop when Microsoft refuses the sync
The engine SHALL stop a run before any further send or deletion when the sync service refuses it, SHALL start
deletions only after the run's prerequisite check, read-back and Graph calls succeeded, and SHALL never turn
DirSync or a sync feature on outside `entra init`.

#### Scenario: Tenant moved to Cloud Sync
- **WHEN** the prerequisite check or the first send gets a SOAP fault or HTTP 403 with a valid token
- **THEN** the run logs `sync_service_refused` with Microsoft's message, sends and deletes nothing more, leaves the
  state unchanged and exits non-zero

#### Scenario: Refusal during deletions
- **WHEN** the second of five deletions is refused by the service
- **THEN** the remaining three are not attempted and their state rows stay

#### Scenario: DirSync turned off in the tenant
- **WHEN** `entra sync --service` finds DirSync off
- **THEN** it does not turn it back on, logs that `ad-manager entra init` is needed, and retries at its interval

#### Scenario: Hard-match blocked for a role holder
- **WHEN** the service refuses a user because the matching cloud user holds an Entra role
- **THEN** the log has `hard_match_blocked` with the way out (remove the role, sync, restore it), the user is not
  recorded as sent, and the other objects are synced

### Requirement: Delta, retries and dry run
The engine SHALL send only objects and hashes that changed since the last successful send (or all with
`--force`), SHALL record a send only after Microsoft accepted it, and in dry run SHALL write nothing to AD,
Microsoft or the state database.

#### Scenario: Unchanged second run
- **WHEN** `entra sync` runs twice with no AD change
- **THEN** the second run sends no object and no hash

#### Scenario: Nested group created in the same run
- **WHEN** a new group `Sales` has the new group `Sales-EU` as a member
- **THEN** `Sales` is sent, then sent again after 30 s, and recorded only after the second send succeeds

#### Scenario: Hash before the user exists
- **WHEN** `ProvisionCredentials` returns a `Result` error for three users created in this run, with no nested
  group wait before
- **THEN** the run waits 30 s once (not per user), retries each once, logs the ones that fail again and retries
  their hashes next run

#### Scenario: Dry run
- **WHEN** `entra sync --dry-run` runs with changes pending
- **THEN** it prints the sends, deletions, rotations and AD writes it would make, and the AD, the tenant, the
  identity directory and the state database are unchanged

#### Scenario: Python state imported
- **WHEN** `entra import-state` reads a `last_send_azuread.db` with 300 users and 40 groups
- **THEN** the next run sends no object that did not change, sends every hash once, and the source file is not
  modified

### Requirement: Secrets never written
`entra` SHALL keep delegated admin tokens and the AD password in memory only, and SHALL never write a token, a
password, an NT hash or the AAD hash to logs, audit, the state database or the terminal.

#### Scenario: Log after a hash send
- **WHEN** a hash is sent for `luke.skywalker`
- **THEN** the log line has the anchor and the account name, and no log, audit or state row contains the NT hash
  or the AAD hash, and the state row's digest cannot be recomputed without `.state-key`

#### Scenario: Callback config refused
- **WHEN** the configuration has `folder_callback_python`
- **THEN** the run refuses to start with "Python callbacks are not supported by ad-manager entra"
