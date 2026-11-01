## MODIFIED Requirements

### Requirement: Audit
Every change made through the panel or the CLI SHALL produce an audit line with date/time, operator, source IP (panel), action, target object and result, without password values. The SSH terminal is the one exception: the panel audits the opening and closing of each terminal (`ssh.open`, `ssh.close`), not the commands run in it, which the server's own login records cover.

#### Scenario: Password change audit
- **WHEN** the operator resets the password of `john.smith`
- **THEN** the audit log records the operator, `password.reset`, `john.smith` and `ok`, and does not contain the password

#### Scenario: Command in the SSH terminal
- **WHEN** the operator runs `samba-tool user delete john.smith` in the SSH terminal
- **THEN** the audit log has the terminal's `ssh.open` and `ssh.close` lines and no `user.delete` line
