# MySQL/MariaDB Backup

Go program for backing up all MySQL/MariaDB databases with configurable
retention (daily/weekly/monthly/yearly), optional remote copy via SFTP, and
error email notifications. Configured via [janmz/sconfig](https://github.com/janmz/sconfig)
(JSON) with secure password handling.

**Donationware for CFI Kinderhilfe.** License: MIT with attribution.

## Features

- Backs up all user databases (excluding `information_schema`,
  `performance_schema`, `mysql`).
- Exports users and grants (MariaDB: `mysqldump --system=users`,
  MySQL: `mysqlpump --users`), parses the output, and appends relevant
  CREATE USER/GRANT blocks to each database dump (root is not included).
- One ZIP per database: `mysql_backup_<yyyymmdd>_<hostname>_<databasename>.zip`
 containing a single SQL file (dump + user block + `FLUSH PRIVILEGES`).
- Retention: keep last N daily/weekly/monthly/yearly backups (weekly = Sunday,
  monthly = last day of month, yearly = 31 Dec).
- Optional remote backup via SFTP.
- Critical error notification by email (low disk space, MySQL unreachable,
  remote copy failure).
- **Automatic schedule setup** on first run: Windows Task Scheduler or Linux
  systemd timer (no separate install step required).
- Cross-platform: Windows and Linux (paths and scheduling adapt automatically).
- If MySQL was auto-started for the backup, it is always stopped on exit (even on
  error). When running as a debug binary (e.g. `__debug_bin.exe`), schedule init
  does not create or update the Windows task (so the task is not overwritten).

## Configuration

Copy `config.example.json` to `config.json` and set:

| Field | Description |
| ----- | ----------- |
| `mysql_host`, `mysql_port` | MySQL/MariaDB server |
| `mysql_bin` | Optional: directory containing mysql, mysqldump, mysqlpump (e.g. `D:\xampp\mysql\bin`) when not in PATH |
| `mysql_auto_start_stop`, `mysql_start_cmd`, `mysql_stop_cmd` | Optional: If MySQL is not running (e.g. XAMPP), start before backup and stop after. Example: `mysql_start_cmd`: `C:\xampp\mysql_start.bat`, `mysql_stop_cmd`: `C:\xampp\mysql_stop.bat` |
| `mysql_data_dir` | Data directory of the instance (required for `--restorefull`) |
| `mysql_backup_dir` | Optional template backup directory of the instance for data initialization. If empty, sibling `backup` next to `mysql_data_dir` is used |
| `root_password` / `root_secure_password` | Root password (sconfig encrypts into `root_secure_password`) |
| `retain_daily`, `retain_weekly`, `retain_monthly`, `retain_yearly` | How many backups to keep per period |
| `backup_dir` | Local backup directory |
| `log_filename` | Log file path (default: `backup_dir/mysqlbackup.log`) |
| `admin_email`, `admin_smtp_*` | Error notification email and SMTP. `admin_smtp_tls`: `"tls"` (port 465, implicit TLS), `"starttls"` (port 587), `""` = auto |
| `remote_backup_dir`, `remote_ssh_*` | Optional SFTP remote backup. |
| `remote_ssh_host_key` | **Mandatory if using remote.** Path to known_hosts file or inline key line(s). Multiple keys: separate with double vertical bar. On mismatch the log shows the server key to add. |
| `start_time` | Daily run time (HH:MM, 00:00–23:59, default 22:00) for schedule |

**Config validation (warnings only):** Config file max 10 KiB. Ports
(mysql_port, admin_smtp_port, remote_ssh_port) recommended 1–65535;
retain_* 1–364; start_time HH:MM. Invalid values are still used but a
warning is logged.

Config file is looked up in: `-config` path, then current directory
(`config.json`), then user home.

## Usage

```bash
# Show status (config, backup dates, job) – default when no flag is given
mysqlbackup
mysqlbackup --status
mysqlbackup --status -config /path/to/config.json

# Run backup (scheduled jobs; manual runs overwritten by next nightly job)
mysqlbackup --backup
mysqlbackup --backup -config /path/to/config.json

# Restore from latest backup day (all ZIPs of that date)
mysqlbackup --restore

# Restore from latest backup day before a date
mysqlbackup --restore 20250210

# Full restore (stop mysql, data->data.old, copy instance backup->data, import)
mysqlbackup --restorefull

# Full restore from latest backup before a date
mysqlbackup --restorefull 20250210

# Create scheduled jobs (Windows Task Scheduler / Linux systemd timer)
mysqlbackup --init

# Remove scheduled jobs
mysqlbackup --remove

# Write config file with plaintext passwords (for migration/inspection)
mysqlbackup --cleanconfig

# Download backup file(s) from remote (filename or wildcards, no path components)
mysqlbackup --getfile "mysql_backup_*.zip"
```

## Restore

Each ZIP contains one SQL file (e.g. `mydb.sql`).

### Restore modes

- `--restore`: imports from the latest backup day (or latest backup day before
  optional trailing `YYYYMMDD`).

- `--restorefull`: full reinit flow for MySQL/MariaDB instances that provide a
  template `backup` directory:
  - stop server if running
  - rename `data` to `data.old`
  - copy instance `backup` directory to `data`
  - start server (root usually empty in template)
  - import selected backup ZIPs

Manual restore from a single ZIP:

```bash
unzip mysql_backup_20250131_localhost_mydb.zip
mysql -u root -p < mydb.sql
```

The SQL includes the database dump and the users/grants that have privileges on
that database (root is not included).

## Security

- **SSH/SFTP:** Host key verification is required. Set `remote_ssh_host_key`
  to a known_hosts file path or inline key line(s). Multiple keys (e.g.
  ecdsa and ed25519) can be separated by double vertical bar so the
  connection works regardless of which key type the server sends. On host
  key mismatch the log prints the key the server sent so you can add it.
- **Passwords:** MySQL password is passed via the `MYSQL_PWD` environment
  variable (not on the process command line). Config supports sconfig-
  encrypted passwords. With `-v`/`--verbose`, command lines logged by the
  schedule layer have password arguments redacted.
- **Config:** Max config file size 10 KiB. Validation warnings for ports
  (1–65535), retain (1–364), and start_time (00:00–23:59).
- **Paths:** Backup filenames sanitize database names (no path traversal).
  `--getfile` accepts only base filenames (no `..` or path separators).
- **Audit:** See `securityreport.md` for the full security audit and GDPR
  notes.

## Requirements

- Go 1.21+
- `mysql` and `mysqldump` (and for MySQL user export: `mysqlpump` or fallback
  without user passwords) in PATH
- Windows: Task Scheduler (schtasks). Linux: systemd (user or system).

## Build

```bash
go mod tidy
go build -o mysqlbackup .
# Windows: mysqlbackup.exe
# Linux: ./mysqlbackup
```

## CI/CD

See `.github/workflows/build.yml` for build and lint.
