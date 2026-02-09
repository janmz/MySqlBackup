# MySQL/MariaDB Backup

Go-Programm zum Sichern aller MySQL/MariaDB-Datenbanken mit konfigurierbarer Aufbewahrung (täglich/wöchentlich/monatlich/jährlich), optionalem Remote-Kopie per SFTP und Fehler-E-Mail. Konfiguration über [janmz/sconfig](https://github.com/janmz/sconfig) (JSON) mit sicherer Passwortverwaltung.

**Donationware für CFI Kinderhilfe.** Lizenz: MIT mit Namensnennung.

## Funktionen

- Sichert alle Benutzer-Datenbanken (ohne `information_schema`, `performance_schema`, `mysql`).
- Exportiert User und Grants (MariaDB: `mysqldump --system=users`, MySQL: `mysqlpump --users`), parst die Ausgabe und hängt die zugehörigen CREATE USER/GRANT-Blöcke an jeden DB-Dump an (root bleibt außen vor).
- Ein ZIP pro Datenbank: `mysql_backup_<yyyymmdd>_<hostname>_<databasename>.zip` mit einer SQL-Datei (Dump + User-Anhang + `FLUSH PRIVILEGES`).
- Aufbewahrung: die letzten N täglichen/wöchentlichen/monatlichen/jährlichen Backups (wöchentlich = Sonntag, monatlich = letzter Tag im Monat, jährlich = 31.12.).
- Optionales Remote-Backup per SFTP.
- E-Mail bei kritischen Fehlern (Speicherplatz, MySQL nicht erreichbar, Remote fehlgeschlagen).
- **Automatische Einrichtung des Zeitplans** beim ersten Lauf: Windows Task Scheduler oder Linux systemd-Timer (kein separates Install-Kommando nötig).
- Plattformunabhängig: Windows und Linux (Pfade und Zeitplan passen sich an).

## Konfiguration

Kopiere `config.example.json` nach `config.json` und setze:

| Feld | Beschreibung |
|------|--------------|
| `mysql_host`, `mysql_port` | MySQL/MariaDB-Server |
| `mysql_bin` | Optional: Verzeichnis mit mysql, mysqldump, mysqlpump (z. B. `D:\xampp\mysql\bin`), wenn nicht im PATH |
| `mysql_auto_start_stop`, `mysql_start_cmd`, `mysql_stop_cmd` | Optional: Wenn MySQL nicht läuft (z. B. XAMPP), vor Backup starten und danach wieder stoppen. Beispiel: `mysql_start_cmd`: `C:\xampp\mysql_start.bat`, `mysql_stop_cmd`: `C:\xampp\mysql_stop.bat` |
| `root_password` / `root_secure_password` | Root-Passwort (sconfig verschlüsselt in `root_secure_password`) |
| `retain_daily`, `retain_weekly`, `retain_monthly`, `retain_yearly` | Wie viele Backups pro Periode behalten |
| `backup_dir` | Lokales Backup-Verzeichnis |
| `log_filename` | Log-Datei (Standard: `backup_dir/mysqlbackup.log`) |
| `admin_email`, `admin_smtp_*` | E-Mail und SMTP für Fehlermeldungen. `admin_smtp_user`: optionaler Login (sonst = admin_email). `admin_smtp_tls`: `"tls"` (Port 465), `"starttls"` (Port 587), `""` = Auto |
| `remote_backup_dir`, `remote_ssh_*` | Optionales SFTP-Remote-Backup |
| `start_time` | Tägliche Startzeit (HH:MM, Standard 22:00) für den Zeitplan |

Die Config-Datei wird gesucht in: `-config`-Pfad, dann aktuellem Verzeichnis (`config.json`), dann Benutzer-Home.

## Aufruf

```bash
# Status anzeigen (Config, Backupdateien, Job) – Standard ohne Flag
mysqlbackup
mysqlbackup --status
mysqlbackup --status -config /pfad/zur/config.json

# Backup ausführen (wird von den Jobs übergeben; manuell erzeugte Dateien werden vom nächsten Nachtlauf überschrieben)
mysqlbackup --backup
mysqlbackup --backup -config /pfad/zur/config.json

# Geplante Jobs anlegen (Windows Task Scheduler / Linux systemd-Timer)
mysqlbackup --init

# Geplante Jobs entfernen
mysqlbackup --remove

# Config-Datei mit Klartextpasswörtern schreiben (z. B. Migration/Prüfung)
mysqlbackup --cleanconfig
```

## Wiederherstellung

Jedes ZIP enthält eine SQL-Datei (z. B. `mydb.sql`). Wiederherstellen mit:

```bash
unzip mysql_backup_20250131_localhost_mydb.zip
mysql -u root -p < mydb.sql
```

Die SQL enthält den DB-Dump und die User/Grants, die Rechte auf diese Datenbank haben (root nicht enthalten).

## Anforderungen

- Go 1.21+
- `mysql` und `mysqldump` (und für MySQL User-Export: `mysqlpump` oder Fallback ohne User-Passwörter) im PATH
- Windows: Task Scheduler (schtasks). Linux: systemd (User oder System).

## Build

```bash
go mod tidy
go build -o mysqlbackup .
# Windows: mysqlbackup.exe
# Linux: ./mysqlbackup
```

## CI/CD

Siehe `.github/workflows/build.yml` für Build und Lint.
