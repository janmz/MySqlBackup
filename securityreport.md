# Sicherheitsbericht MySqlBackup

## Security Check 2026-02-26 15:30:00 (Re-Audit)

### Self-Improvement (geprüfte Quellen)

- [OWASP Secure by Design](https://owasp.org/www-project-secure-by-design-framework/)
- [CNIL GDPR Developer's Guide](https://www.cnil.fr/en/gdpr-developers-guide)
- [EDPB](https://www.edpb.europa.eu/edpb_en)
- [Manifest.ly GDPR Checklist](https://www.manifest.ly/use-cases/software-development/gdpr-compliance-checklist)

Ich habe die genannten URLs geprüft und keine neuen, für die Prüfliste
relevanten Punkte gefunden.

### Scope (Re-Audit)

- Sensible Daten: Log (inkl. Debug), Fehlermeldungen, Prozessliste.
- Eingabe- und Config-Validierung, Path-Traversal (getfile, Backup-Dateinamen).
- Externe Aufrufe: MySQL, SSH/SFTP, schtasks/PowerShell, systemd/cron – Injection,
  Escaping.
- Abhängigkeiten: govulncheck.
- Offene Punkte aus dem letzten Audit.

### Übersicht: erledigt / offen / neu

| Thema | Status | Anmerkung |
| ----- | ------ | --------- |
| Passwort im Debug-Log | **erledigt** | `redactArgsForLog` in schedule.go maskiert `-p`, `--password=` vor Log. |
| Path-Traversal DB-Namen | **erledigt** | `dbNameForFile` in backup.go sanitisiert DB-Namen für ZIP-Dateinamen. |
| Config-Validierung | **erledigt** | max 10 KiB, Ports 1024–65535, Retain 1–364, StartTime 00:00–23:59 (Warnungen). |
| SSH Host-Key | **erledigt** | `remote_ssh_host_key` (Datei oder Inline), `InsecureIgnoreHostKey` entfernt. |
| MySQL-Passwort in Prozessliste | **offen** | `-p<Passwort>` weiterhin in Prozesszeile (ps/tasklist). Dokumentiert. |
| Abhängigkeiten | **erledigt** | govulncheck: keine Schwachstellen. |

### Findings (nach Priorität)

- **Mittel (weiterhin offen):** **MySQL-Passwort in Prozessliste** – Passwort wird
  als `-p<Passwort>` an mysql/mysqldump übergeben; unter Linux/Windows für
  andere Nutzer sichtbar (ps/tasklist). Status: acknowledged. Nächster Schritt:
  Dokumentation; optional Passwort per MYSQL_PWD oder temporäre Option-Datei.

- **Niedrig:** **log.start.arguments** – Bei Start wird `os.Args[1:]` geloggt
  (main.go, logStartup). Enthält z. B. `-config <pfad>`; keine Passwörter.
  Status: acknowledged.

- **Bestätigt (positiv):** Debug-Log redigiert (`redactArgsForLog`). DB-Namen
  für Dateinamen mit `dbNameForFile` bereinigt. getfile mit
  `validGetfilePattern` (main.go, remote.go). Config: maxConfigSize 10 KiB,
  Validate() mit Port-/Retain-/StartTime-Warnungen. SSH: hostKeyCallback aus
  `remote_ssh_host_key` (Datei oder Inline), kein InsecureIgnoreHostKey.
  PowerShell: `escapeForPSSingleQuoted` für Pfade/Argumente;
  `pathForTR` für cmd; startTime auf "HH:MM" normalisiert (kein Quote-Breakout).
  resolve_unc_windows: drive nur "X:", kein User-Input. MySQL-Aufrufe werden
  nicht mit Debug geloggt (nur Schedule-Befehle über runWithDebug).

### Dependency Risk Summary

- `govulncheck ./...`: **Keine gefundenen Schwachstellen.**

### Recommended Next Actions

1. Optional: MySQL-Passwort nicht in Prozesszeile (MYSQL_PWD oder
   Option-Datei mit sicheren Rechten); Risiko in Doku erwähnen.
2. Optional: Sensible Pfade in log.start.arguments redigieren, falls
  gewünscht.

### Existing Security Measures Confirmed

- Config- und Pfad-Normalisierung (filepath.Clean/FromSlash).
- getfile: nur Dateinamen ohne Pfadkomponenten.
- Restore-Pfade aus retention.ListBackups (dateibasiert).
- Task-Namen Konstanten; PowerShell-Strings escaped; schtasks-Argumente mit
  pathForTR.
- Fehlermeldungen über i18n; keine Roh-Stacktraces an Endnutzer.
- sconfig für verschlüsselte Passwörter in Config.

### GDPR (DSGVO) – Kurzbewertung (aktualisiert)

- **Integrität/Vertraulichkeit:** Partial → **Compliant** – Passwort nicht mehr
  im Log; SSH mit Host-Key-Prüfung. Verbleibendes Risiko: Passwort in
  Prozessliste (betrifft lokale Nutzer mit Prozess-Einsicht).
- **Art. 32 Sicherheit:** Partial → **Compliant** – Maßnahmen (Log-Redaktion,
  SSH Host-Key, Config-Validierung) umgesetzt.

---

## Security Check 2026-02-26 (Audit)

### Self-Improvement (geprüfte Quellen)

- [OWASP Secure by Design](https://owasp.org/www-project-secure-by-design-framework/)
  – Framework zu Secure-by-Design (Design-Phase), Prinzipien und
  Architektur-Checklisten.
- [CNIL GDPR Developer's Guide](https://www.cnil.fr/en/gdpr-developers-guide) –
  CNIL Developer's Guide (Sheets zu Datenschutz, Minimierung, Rechte,
  Aufbewahrung).
- [EDPB](https://www.edpb.europa.eu/edpb_en) – EDPB (Aktuelles, Guidelines).
- [GDPR-Info](https://gdpr-info.eu/) – Abruf lief ins Timeout; nicht
  ausgewertet.
- [Manifest.ly GDPR Checklist](https://www.manifest.ly/use-cases/software-development/gdpr-compliance-checklist)
  – Checkliste Software-Entwicklung/GDPR.

Ich habe die genannten URLs geprüft und keine neuen, für die Prüfliste
relevanten Punkte gefunden, die die bestehenden Audit-Schritte ändern würden.

### Scope

- **Eingaben:** JSON-Config (config.json), CLI-Flags (-config, --backup, etc.),
  optionales Datumsargument (YYYYMMDD), -getfile-Dateiname.
- **Auth/Flows:** Keine Nutzer-Auth; Config enthält DB-Root, SMTP, SSH/SFTP,
  optional sconfig-verschlüsselte Passwörter.
- **DB:** Zugriff nur über mysql/mysqldump/mysqlpump-CLI (exec).
- **Dateizugriff:** Backup-Verzeichnis, Log, Config, Remote-SFTP, Restore-ZIPs.
- **Externe Aufrufe:** MySQL-CLI, SSH/SFTP, schtasks/PowerShell, systemd,
  crontab, Start/Stop-Befehle (z. B. XAMPP).
- **Logging:** Datei-Log + optional Stdout; bei -v/--verbose [DEBUG] inkl.
  Exec-Befehle und Ausgaben.

### Findings (nach Priorität)

- **Hoch:** Bei **-v/--verbose** werden in `runWithDebug` Befehl und Args
  geloggt (`internal/schedule/schedule.go`, Zeile 36). MySQL/MariaDB werden mit
  `-p<Passwort>` aufgerufen (`internal/mysql/mysql.go`, baseArgs). **Passwort
  landet im Log.** Status: offen. Nächster Schritt: In Debug-Log die
  Kommandozeile ohne Passwort ausgeben (z. B. Args filtern oder Platzhalter
  `-p***`).

- **Mittel:** **MySQL-Passwort in Prozessliste:** Passwort wird als
  `-p<Passwort>` (ein Argument) an mysql/mysqldump übergeben; unter Linux/Unix
  ist die Kommandozeile für andere lokale Nutzer sichtbar (ps). Status: offen.
  Nächster Schritt: Dokumentieren; optional Passwort per Umgebungsvariable oder
  temporäre Option-Datei übergeben (mit sicheren Berechtigungen).

- **Mittel:** **SSH Host Key nicht verifiziert** (`internal/remote/remote.go`,
  Zeile 260): `HostKeyCallback: ssh.InsecureIgnoreHostKey()` – anfällig für
  MITM bei SFTP. Status: offen. Nächster Schritt: Host-Key prüfen (z. B.
  KnownHosts oder konfigurierbarer Fingerprint); InsecureIgnoreHostKey nur
  optional und dokumentiert.

- **Mittel:** **Datenbankname in Dateinamen:** In `internal/backup/backup.go`
  wird der DB-Name ungefiltert in den ZIP-Dateinamen übernommen
  (`mysql_backup_<date>_<host>_<db>.zip`). Enthält der DB-Name z. B. `..` oder
  Pfadzeichen, kann `filepath.Join(backupDir, zipName)` aus dem Backup-Verzeichnis
  herausführen (Path Traversal). Status: offen. Nächster Schritt: DB-Namen für
  Dateinamen sanitisieren (analog zu `hostnameForFile`, z. B. nur erlaubte
  Zeichen).

- **Niedrig:** **Konfigurationsvalidierung:** Ports, Retain-Werte und
  StartTime werden nicht explizit begrenzt (z. B. Port 0 oder negative
  retain_daily). Defaults und Clean/Normalize mildern das; Missbrauch führt
  eher zu Fehlverhalten als zu kritischen Lecks. Status: offen. Nächster
  Schritt: Optionale Validierung (z. B. Port 1–65535, retain >= 0,
  StartTime HH:MM).

- **Bestätigt (positiv):** Sensible Daten werden nicht in normale Log-Zeilen
  geschrieben (nur in Debug bei -v). getfile-Argument wird gegen Path
  Traversal geprüft (`validGetfilePattern`). PowerShell/schtasks-Argumente
  werden für Anführungszeichen escaped (`pathForTR`, `escapeForPSSingleQuoted`).
  Keine Stacktraces oder Roh-Fehler an Endnutzer; Fehlermeldungen über i18n.
  Passwörter in Config optional per sconfig verschlüsselt.

### Dependency Risk Summary

- `govulncheck ./...`: **Keine gefundenen Schwachstellen.**
- Kein composer.json (reines Go-Projekt); Composer-Audit entfällt.
- Relevante Abhängigkeiten: janmz/sconfig, golang.org/x/crypto (SSH/SFTP),
  pkg/sftp – derzeit keine bekannten CVEs gemeldet.

### Recommended Next Actions

1. Debug-Log so anpassen, dass niemals Passwörter (MySQL, SSH, SMTP) in
   Befehlszeilen/Args erscheinen (Platzhalter oder Filter).
2. SSH HostKeyCallback durch verifizierbare Prüfung ersetzen und in Doku
   erwähnen.
3. DB-Namen für Backup-Dateinamen sanitisieren, um Path Traversal zu
   verhindern.
4. Optional: Konfigurationsvalidierung (Ports, Retain, StartTime) und
   Dokumentation des Risikos „Passwort in Prozessliste“.

### Existing Security Measures Confirmed

- Config-Pfade und Backup-/Log-Pfade werden mit `filepath.Clean`/FromSlash
  normalisiert.
- getfile erlaubt nur Dateinamen ohne Pfadkomponenten (kein `..`, keine
  Schrägstriche).
- Restore-Pfade kommen aus `retention.ListBackups` (dateibasiert), nicht aus
  Nutzer-Input.
- Task-Namen (schtasks/systemd/cron) sind Konstanten; StartTime wird auf
  HH:MM geprüft; PowerShell-Strings werden für Single-Quotes escaped.
- Fehler an Nutzer/Admin sind über i18n formuliert; keine Roh-Stacktraces.
- sconfig ermöglicht verschlüsselte Passwörter in der Config.

---

### GDPR (DSGVO) – Kurzbewertung

- **Zweckbindung / Minimierung:** Compliant – Tool verarbeitet nur
  Backup-relevante Daten (DB-Dumps, Config).
- **Integrität/Vertraulichkeit:** Partial – Passwort in Log bei -v; SSH ohne
  Host-Key-Prüfung.
- **Speicherbegrenzung:** Compliant – Retention (retain_*) begrenzt
  Aufbewahrung.
- **Betroffenenrechte:** Not evident – Kein Nutzer-Frontend; Backups können
  personenbezogene DB-Inhalte enthalten – Verantwortung beim Betreiber.
- **Art. 32 Sicherheit:** Partial – Verschlüsselung (sconfig, optional AES
  remote) vorhanden; Verbesserung bei Log und SSH empfohlen.

Personenbezogene Daten in Logs: Bei normalem Betrieb keine; bei --verbose
können Befehlszeilen (inkl. Passwort) ins Log. Backups: Enthalten nur das, was
in den gesicherten Datenbanken steht – Verantwortung für Zweck und
Rechtmäßigkeit liegt beim Betreiber.
