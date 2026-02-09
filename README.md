# MySQL/MariaDB Backup

Go program for backing up all MySQL/MariaDB databases with configurable retention (daily/weekly/monthly/yearly), optional remote copy via SFTP, and error email notifications. Configured via [janmz/sconfig](https://github.com/janmz/sconfig) (JSON) with secure password handling.

**Donationware for CFI Kinderhilfe.** License: MIT with attribution.

## Features

- Backs up all user databases (excluding `information_schema`, `performance_schema`, `mysql`).
- Exports users and grants (MariaDB: `mysqldump --system=users`, MySQL: `mysqlpump --users`), parses the output, and appends relevant CREATE USER/GRANT blocks to each database dump (root is not included).
- One ZIP per database: `mysql_backup_<yyyymmdd>_<hostname>_<databasename>.zip` containing a single SQL file (dump + user block + `FLUSH PRIVILEGES`).
- Retention: keep last N daily/weekly/monthly/yearly backups (weekly = Sunday, monthly = last day of month, yearly = 31 Dec).
- Optional remote backup via SFTP.
- Critical error notification by email (low disk space, MySQL unreachable, remote copy failure).
- **Automatic schedule setup** on first run: Windows Task Scheduler or Linux systemd timer (no separate install step required).
- Cross-platform: Windows and Linux (paths and scheduling adapt automatically).

## Configuration

Copy `config.example.json` to `config.json` and set:

| Field | Description |
|-------|-------------|
| `mysql_host`, `mysql_port` | MySQL/MariaDB server |
| `mysql_bin` | Optional: directory containing mysql, mysqldump, mysqlpump (e.g. `D:\xampp\mysql\bin`) when not in PATH |
| `mysql_auto_start_stop`, `mysql_start_cmd`, `mysql_stop_cmd` | Optional: If MySQL is not running (e.g. XAMPP), start before backup and stop after. Example: `mysql_start_cmd`: `C:\xampp\mysql_start.bat`, `mysql_stop_cmd`: `C:\xampp\mysql_stop.bat` |
| `root_password` / `root_secure_password` | Root password (sconfig encrypts into `root_secure_password`) |
| `retain_daily`, `retain_weekly`, `retain_monthly`, `retain_yearly` | How many backups to keep per period |
| `backup_dir` | Local backup directory |
| `log_filename` | Log file path (default: `backup_dir/mysqlbackup.log`) |
| `admin_email`, `admin_smtp_*` | Error notification email and SMTP. `admin_smtp_tls`: `"tls"` (port 465, implicit TLS), `"starttls"` (port 587), `""` = auto |
| `remote_backup_dir`, `remote_ssh_*` | Optional SFTP remote backup |
| `start_time` | Daily run time (HH:MM, default 22:00) for schedule |

Config file is looked up in: `-config` path, then current directory (`config.json`), then user home.

## Usage

```bash
# Show status (config, backup dates, job) – default when no flag is given
mysqlbackup
mysqlbackup --status
mysqlbackup --status -config /path/to/config.json

# Run backup (used by scheduled jobs; manual runs are overwritten by the next nightly job)
mysqlbackup --backup
mysqlbackup --backup -config /path/to/config.json

# Create scheduled jobs (Windows Task Scheduler / Linux systemd timer)
mysqlbackup --init

# Remove scheduled jobs
mysqlbackup --remove

# Write config file with plaintext passwords (for migration/inspection)
mysqlbackup --cleanconfig
```

## Restore

Each ZIP contains one SQL file (e.g. `mydb.sql`). Restore with:

```bash
unzip mysql_backup_20250131_localhost_mydb.zip
mysql -u root -p < mydb.sql
```

The SQL includes the database dump and the users/grants that have privileges on that database (root is not included).

## Requirements

- Go 1.21+
- `mysql` and `mysqldump` (and for MySQL user export: `mysqlpump` or fallback without user passwords) in PATH
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
