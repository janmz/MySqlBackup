# Changelog

Alle wesentlichen Änderungen am Projekt werden hier dokumentiert.

## [1.0.0.11] – 2026-02-02

### Geändert

- **User-Verteilung auf Backups**: User werden pro Datenbank korrekt zugeordnet. Zuerst werden alle User ausgelesen; an jede Backup-Datei werden nur die User und **die zu dieser DB passenden GRANTs** angehängt. Hat ein Nutzer Rechte auf mehreren Datenbanken, wird er mit den jeweiligen passenden Rechten an jede dieser DB-Backups angehängt (nicht mehr der komplette Block in jede Datei).
- **CREATE USER idempotent**: CREATE USER wird beim Anhängen an den Dump zu **CREATE USER IF NOT EXISTS** umgeschrieben, damit die Wiederherstellung nicht fehlschlägt, wenn der User bereits existiert (z. B. aus einem zuvor wiederhergestellten anderen DB-Backup).

---

## [1.0.0.10] – 2026-02-02

### Hinzugefügt

- **Launch-Konfigurationen**: `.vscode/launch.json` mit Konfigurationen „Status“ und „Backup“ zum direkten Start/Debug von `--status` und `--backup` aus der IDE.

---

## [1.0.8] – 2025-01-31

### Geändert

- **Benennung bei localhost**: Wenn die Datenbank über `localhost` (oder `127.0.0.1`) angesprochen wird und `mysql_hostname` in der Config gesetzt ist, wird dieser Wert für die Backup-Dateinamen verwendet (z. B. `mysql_backup_20250131_NOAH_mydb.zip`). Beispiel-Config und Kommentar ergänzt.

---

## [1.0.7] – 2025-02-02

### Geändert

- **MariaDB**: Option `--set-gtid-purged=OFF` wird bei MariaDB nicht mehr an mysqldump übergeben (MariaDB kennt die Option nicht → „unknown variable“). Bei MySQL weiterhin gesetzt.

---

## [1.0.6] – 2025-02-02

### Hinzugefügt

- **mysql_bin** in Config: optionales Verzeichnis mit mysql, mysqldump, mysqlpump (z. B. `D:\xampp\mysql\bin`), wenn die Befehle nicht im PATH liegen.

---

## [1.0.5] – 2025-02-02

### Geändert

- **MySQL-Lifecycle**: Wenn `mysql`-CLI nicht erreichbar ist, aber der MySQL-Port (z. B. 3306) offen ist, wird kein Start mehr versucht (XAMPP meldet „läuft“, Programm ging trotzdem in Start → Fehler). Stattdessen: TCP-Check auf host:port; wenn offen → Backup fortsetzen (evtl. `mysql` nicht im PATH).
- **SMTP**: Optionaler `admin_smtp_user` (Login, falls abweichend von `admin_email`). Auth mit Identity = Username (beides Login), viele Server (z. B. kasserver) erwarten das so.

---

## [1.0.4] – 2025-02-02

### Hinzugefügt

- SMTP-Verbindung konfigurierbar: `admin_smtp_tls` = `"tls"` (implizites TLS, Port 465), `"starttls"` (Port 587). Ohne Angabe: 465→tls, 587→starttls. Behebt „535 authentication failed“, wenn der Server STARTTLS verlangt.

---

## [1.0.3] – 2025-01-31

### Geändert

- Windows: Geplanten Task ohne `/RU SYSTEM` anlegen, damit `--init` ohne Administratorrechte funktioniert (Task läuft unter aktuellem Benutzer).

---

## [1.0.2] – 2025-01-31

### Hinzugefügt

- MySQL-Lifecycle (z. B. XAMPP): Prüfung, ob MySQL läuft; wenn nicht und `mysql_auto_start_stop` aktiv, Start mit `mysql_start_cmd`, nach Backup Stopp mit `mysql_stop_cmd`. Config: `mysql_auto_start_stop`, `mysql_start_cmd`, `mysql_stop_cmd`. Lokale `mysqlbackup_config.json` für XAMPP mit typischen Pfaden (`C:\xampp\mysql_start.bat` / `mysql_stop.bat`).

---

## [1.0.1] – 2025-01-31

### Geändert

- Kommandozeile auf Flags umgestellt: `--init` (Jobs erstellen), `--cleanconfig` (Config mit Klartextpasswörtern schreiben), `--remove` (Jobs löschen), `--status` (Config prüfen, Backupdateien, Job-Einstellung anzeigen), `--backup` (Backup ausführen; wird von Jobs übergeben). Ohne Flag wird `--status` ausgeführt.
- Jobs rufen das Programm mit `--backup -config <pfad>` auf. Manuell mit `--backup` erstellte Dateien werden vom nächsten erfolgreichen Nachtlauf überschrieben.

---

## [1.0.0] – 2025-01-31

### Hinzugefügt

- Go-Programm für MySQL/MariaDB-Backup mit Konfiguration über janmz/sconfig (JSON).
- Sichere Passwörter (root_password, admin_smtp_password, remote_ssh_password) via sconfig-Paare.
- Backup aller Benutzer-Datenbanken (ohne information_schema, performance_schema, mysql).
- User/Grants-Export (MariaDB: mysqldump --system=users, MySQL: mysqlpump --users), Parsing und Anhängen an jeden DB-Dump (root bleibt automatisch außen vor).
- Ein ZIP pro Datenbank: `mysql_backup_<yyyymmdd>_<hostname>_<databasename>.zip` mit SQL (Dump + User-Anhang + FLUSH PRIVILEGES).
- Retention: täglich/wöchentlich/monatlich/jährlich mit konfigurierbaren Anzahlen (retain_daily, retain_weekly, retain_monthly, retain_yearly).
- Optionales Remote-Backup per SFTP (remote_backup_dir, remote_ssh_*).
- Fehler-E-Mail an admin_email bei kritischen Fehlern (Speicherplatz, MySQL nicht erreichbar, Remote fehlgeschlagen).
- Automatisches Einrichten des Zeitplans beim ersten Lauf (Windows: Task Scheduler, Linux: systemd-Timer).
- Subkommandos: `install-schedule`, `uninstall-schedule`.
- Plattformunabhängig: Windows und Linux (filepath, runtime.GOOS).

### Abhängigkeiten

- github.com/janmz/sconfig
- golang.org/x/crypto/ssh
- github.com/pkg/sftp
