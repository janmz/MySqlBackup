# Changelog

Alle wesentlichen Änderungen am Projekt werden hier dokumentiert.

## [1.1.1.50] – 2026-02-05

### Behoben

- **Crontab (Linux)**: Pfade mit Leerzeichen (Programmpfad und Config-Pfad) werden in Cron-Einträgen jetzt korrekt maskiert (einfache Anführungszeichen, interne `'` als `'\''` escaped), sodass der Cron-Job zuverlässig ausgeführt wird.

---

## [1.1.1.49] – 2026-02-05

### Geändert

- **Classify**: `Classify(t)` und `ClassifyLabel(t)` zu einer Funktion **Classify(t time.Time) string** zusammengeführt. Rückgabe ist der lokalisierte Perioden-String (z. B. „täglichen“). Typ `Period` und Konstanten entfernt. Tests prüfen nun den zurückgegebenen String per i18n-Key.
- **i18n vollständig**: Alle verbleibenden fest codierten Texte laufen über i18n (de/en/fr/nl): Fehlermeldungen in remote, schedule, retention, backup, email, config (inkl. „Hardware-ID“, SSH/SFTP, crontab, schtasks, ZIP/Dump, TLS/dial/STARTTLS usw.).

---

## [1.1.1.48] – 2026-02-05

### Geändert

- **Retention-Anzeige**: Neue Funktion `ClassifyLabel(t)` liefert die Periode als lokalisierten String (über i18n). Deutsch: „täglichen“, „wöchentlichen“, „monatlichen“, „jährlichen“. Status-Ausgabe nutzt nun `ClassifyLabel` statt Switch; en/fr/nl angepasst (nl: attributive Form mit -e).

---

## [1.1.1.47] – 2026-02-05

### Geändert

- **Retention-Logik**: Die Aufbewahrung arbeitet jetzt nach **Zeitfenstern** (Kalenderdatum), nicht mehr nach „letzte N Dateien“ pro Periode. **retain_daily 14** = alle Daily-Backups der letzten 14 **Tage** bleiben erhalten (nach Backup-Datum). **retain_weekly 3** = alle Weekly-Backups der letzten 3 **Sonntage**; **retain_monthly/yearly** = letzte N Monatsenden bzw. 31.12. So werden bei mehreren DBs pro Tag/Woche nicht mehr zu viele Backups gelöscht (z. B. 3 Tage alte Backups bei „14 Tage aufbewahren“). Zusätzlich: Datum aus dem Dateinamen wird in Local-Zeitzone geparst (`ParseInLocation`), damit Tagesgrenzen stimmen.

---

## [1.1.1.46] – 2026-02-04

### Geändert

- **Linux/Symlink**: Wenn das Programm per Pfad aufgerufen wird (z. B. `./mysqlbackup` in einem Unterverzeichnis, in dem `mysqlbackup` ein symbolischer Link auf das Oberverzeichnis ist), wird die Config nun aus dem **Aufrufverzeichnis** (wo der Symlink liegt) gesucht, nicht aus dem Verzeichnis des aufgelösten Binärs. Reihenfolge ConfigPath: `-config`-Flag, dann **Verzeichnis des Aufrufpfads** (argv[0], falls mit Pfad gestartet), dann Executable-Verzeichnis, dann aktuelles Verzeichnis, dann Home. Zusätzlich: Chdir erfolgt erst **nach** der Config-Auswahl in das Verzeichnis der gewählten Config-Datei (nicht mehr sofort ins Executable-Verzeichnis), damit das Aufrufverzeichnis bei der Suche korrekt ist.

---

## [1.1.1.45] – 2026-02-04

### Geändert

- **MySQL-Start (Lifecycle)**: Der Start-Befehl (z. B. `mysqld --standalone`) wird nicht mehr bis zu seinem Ende gewartet, weil der Daemon im Vordergrund läuft und nie endet. Er wird jetzt im Hintergrund gestartet (`Start()` ohne `Wait()`), Stdout/Stderr nach DevNull umgeleitet; danach prüft `waitForMySQL` wie bisher, ob der Port erreichbar ist. Stop-Befehl bleibt unverändert (Warten auf Ende mit Timeout).

---

## [1.1.1.44] – 2026-02-04

### Geändert

- **--cleanconfig -verbose**: `--cleanconfig` berücksichtigt jetzt `-verbose`/`--verbose`. Bei gesetztem Flag wird der debug-Parameter an sconfig durchgereicht (sconfig kann dann Debug-Ausgaben liefern) und eine [DEBUG]-Meldung auf stderr ausgegeben.

---

## [1.1.0.42] – 2026-02-04

### Geändert

- **Start-Header**: Beim Start wird zusätzlich der volle (absolute) Pfad der zu ladenden Config-Datei ausgegeben (nach Version, Executable und Aufrufparametern).

---

## [1.1.0.41] – 2026-02-03

### Geändert

- **Arbeitsverzeichnis beim Start**: Beim Start wird das aktuelle Verzeichnis auf das Verzeichnis des Executables gewechselt (`os.Chdir(filepath.Dir(Executable()))`), damit relative Pfade (Config, Log, Backup-Verzeichnis in der Config) unabhängig vom Aufrufort konsistent aufgelöst werden.

---

## [1.1.0.40] – 2026-02-03

### Geändert

- **Windows Task – Exitcode 0x1 behoben**: Die geplante Aufgabe führt den Backup-Befehl nun über `cmd /c "cd /d <workDir> && <exe> --backup -config <config>""` aus. So wird das Arbeitsverzeichnis beim Start gesetzt; relative Pfade (z. B. in der Config) und das Standard-Log funktionieren auch, wenn die Task-Eigenschaft „Arbeitsverzeichnis“ nicht greift. Der Parser für die gespeicherte Task-Aktion unterstützt beide Formate (direkter Aufruf und cmd/cd-Umgebung).

---

## [1.1.0.39] – 2026-02-03

### Geändert

- **Config-Suche und Log-Datei**: Beim Suchen der Config-Datei und als Standard-Ort für die Log-Datei wird immer das Verzeichnis des Executable verwendet, nicht das aktuelle Arbeitsverzeichnis. Reihenfolge ConfigPath: `-config`-Flag, dann Executable-Verzeichnis (config.json), dann aktuelles Verzeichnis, dann Benutzer-Home. Standard-Log (wenn `log_filename` leer): Executable-Verzeichnis/mysqlbackup.log.
- **Windows Task – UNC-Pfade**: Beim Anlegen der geplanten Aufgabe werden Netzwerklaufwerke (z. B. N:\) per PowerShell auf UNC-Pfade (\\server\share\...) umgestellt, damit die Aufgabe auch läuft, wenn der Laufwerksbuchstabe für den Task-Benutzer nicht existiert.

---

## [1.1.0.38] – 2026-02-03

### Geändert

- **Windows Task Scheduler – Einstellungen**: Die PowerShell-Anpassung (WakeToRun, StartWhenAvailable, ExecutionTimeLimit) nutzt nun `New-ScheduledTaskSettingsSet` und `Set-ScheduledTask -TaskName ... -Settings $s` statt `Set-ScheduledTask -InputObject $t`, da mit `-InputObject` die Settings nicht übernommen werden.
- **Verbose/DEBUG-Ausgaben**: Neues Flag `-v` bzw. `-verbose` schaltet detaillierte Ausgaben mit Präfix [DEBUG] ein. Dabei werden alle Exec-Aufrufe (schtasks, PowerShell, systemctl) inkl. Befehl und Ausgabe geloggt. Logger hat neues Feld `Verbose` und Methode `Debug()`.

---

## [1.0.0.36] – 2026-02-03

### Geändert

- **Windows Task Scheduler – Pfadprüfung**: Bei jedem Start mit `--backup` oder `--status` wird geprüft, ob die geplante Aufgabe noch den aktuellen Pfad (Executable/Config) verwendet. Bei Abweichung (z. B. nach Verschieben des Verzeichnisses) wird die Aufgabe gelöscht und mit den aktuellen Pfaden neu angelegt.
- **Windows Task Scheduler – Einstellungen**: Nach Anlegen oder Bestätigung der Aufgabe werden per PowerShell gesetzt: „Computer wecken“ (WakeToRun), „Aufgabe so schnell wie möglich nach verpasstem Start ausführen“ (StartWhenAvailable), maximale Laufzeit 12 Stunden (ExecutionTimeLimit).
- **Windows Task Scheduler – Arbeitsverzeichnis**: Das Arbeitsverzeichnis der Aufgabe (WorkingDirectory) wird auf das Config-Verzeichnis gesetzt, damit relative Log- und Backup-Pfade auch bei UNC-Pfaden (z. B. `\\elisa\daten\...`) und anderem Benutzerkontext korrekt aufgelöst werden.
- **Schedule-Prüfung bei Status**: `--status` führt nun ebenfalls die Schedule-Prüfung aus (EnsureInstalled); bei Bedarf wird die geplante Aufgabe angepasst oder neu angelegt, ohne dass `--init` erneut ausgeführt werden muss.

---

## [1.0.0.35] – 2026-02-03

### Geändert

- **System-Crontab ohne crontab-Programm**: Wenn das Programm „crontab“ nicht vorhanden ist, wird die Zeile automatisch in die System-Crontab eingefügen bzw. dort gelöscht. Es werden nacheinander `/etc/crontab` und `/usr/etc/crontab` verwendet (erstes les-/schreibbares Verzeichnis). Format: `minute hour * * * root /pfad/mysqlbackup --backup -config /pfad`. Beim Entfernen (--remove) wird der Eintrag aus der jeweiligen Datei entfernt. Status erkennt den Eintrag in der System-Crontab.

---

## [1.0.0.34] – 2026-02-03

### Geändert

- **Cron nicht im PATH (z. B. Synology)**: Wenn beim Cron-Fallback „crontab“ nicht gefunden wird (executable not found), gibt das Programm eine klare Fehlermeldung aus und zeigt die exakte Zeile, die manuell eingetragen werden kann (z. B. im Synology Task Scheduler oder nach Installation von cron): „crontab not installed or not in PATH; add this line manually (e.g. via Task Scheduler or crontab -e): …“.

---

## [1.0.0.33] – 2026-02-02

### Geändert

- **Unix-Schedule: Prüfung und Cron-Fallback**: Unter Unix prüft das Programm, ob systemd user („systemctl --user list-timers“) verfügbar ist. Wenn nicht (z. B. keine D-Bus-Session, WSL ohne systemd, Alpine), wird automatisch Cron als Alternative verwendet: Es wird ein Eintrag in der Benutzer-Crontab angelegt (täglich zur konfigurierten Startzeit). Beim Entfernen (--remove) werden sowohl systemd-Timer/Service als auch ein vorhandener Cron-Eintrag (Marker „mysqlbackup-schedule“) entfernt. Status (--status) zeigt bei Cron „Cron (täglich um …)“ mit Befehl. Neue i18n-Keys: job.cron.

---

## [1.0.0.32] – 2026-02-02

### Geändert

- **Start-Header bei allen Aufrufen**: Bei jedem Aufruf (--init, --cleanconfig, --remove, --status, --backup, --getfile sowie bei Nutzungsanzeige) wird derselbe Header wie beim Backup auf stderr ausgegeben: Version, Aufrufpfad (Executable) und Aufrufparameter. So ist bei jeder Ausführung sichtbar, welche Version läuft. Neue i18n-Keys: header.version, header.executable, header.arguments.

---

## [1.0.0.31] – 2026-02-02

### Hinzugefügt

- **Mehrsprachigkeit (i18n)**: Die Anwendung ist mehrsprachig (Deutsch, Englisch [britisch], Französisch, Niederländisch). Die Sprache wird aus LANG/LC_ALL/LANGUAGE ermittelt; unbekannte Sprachen fallen auf Englisch zurück. Übersetzungen sind per go:embed eingebettet (Standalone-Binary). Alle benutzerorientierten Texte (Usage, Fehlermeldungen, Status, Job-Beschreibung) nutzen internal/i18n (T/Tf).

### Geändert

- **schedule.Status**: Gibt jetzt (key string, args []interface{}) für i18n zurück statt einer festen Zeichenkette.

---

## [1.0.0.30] – 2026-02-02

### Geändert

- **--getfile mit Wildcards**: Der Parameter \<dateiname\> darf Wildcards (*, ?) enthalten; die Auswertung erfolgt auf der Remote-Seite (Liste der Backup-ZIPs, dann filepath.Match). Es dürfen keine Pfade im Dateinamen vorkommen (Prüfung in main und remote: kein /, \\, ..). Bei mehreren Treffern werden alle passenden Dateien ins aktuelle Verzeichnis geladen (bestehende Zieldatei → Suffix .lokal). GetFile gibt jetzt eine Liste der gespeicherten Pfade zurück.

---

## [1.0.0.29] – 2026-02-02

### Geändert

- **Parameter-Übersicht**: Wenn kein Aktions-Flag oder ein falsches/unbekanntes Flag angegeben wird, wird eine Übersicht aller Optionen und ihrer Wirkung ausgegeben (statt still auf --status zu wechseln). Die Übersicht wird auch bei -h/-help und bei mehreren Aktions-Flags angezeigt.

---

## [1.0.0.28] – 2026-02-02

### Hinzugefügt

- **--getfile \<dateiname\>**: Lädt eine ZIP-Backup-Datei vom Remote-Server ins aktuelle Verzeichnis. Nur Backup-ZIP-Dateinamen (mysql_backup_YYYYMMDD_*.zip) erlaubt. Bei verschlüsselten Remote-Dateien (salt+nonce+ciphertext) wird mit remote_aes_password entschlüsselt. Wenn der Zieldateiname lokal bereits existiert, wird die geladene Datei mit Suffix „.lokal“ gespeichert (z. B. mysql_backup_xxx.zip.lokal).

---

## [1.0.0.27] – 2026-02-02

### Geändert

- **Remote**: Nur .zip-Dateien werden kopiert oder auf dem Remote-Server gelöscht (explizite Prüfung `filepath.Ext == ".zip"` zusätzlich zum Backup-Regex). Beim Sync wird ins Log geschrieben, ob AES-Verschlüsselung aktiv ist oder nicht („Remote: AES-Verschlüsselung aktiv“ / „Remote: keine AES-Verschlüsselung“).

---

## [1.0.0.26] – 2026-02-02

### Geändert

- **Status Spalten**: Doppeltes Datum entfernt (nur noch ModTime als Datum/Zeit). Alle Spalten mit fester Breite: Datum/Zeit 19 Zeichen, Größe 5 Zeichen rechtsbündig (max. 1023T), Dateiname 50 Zeichen, Art 12 Zeichen. Summenzeile: „Summe:“ und Gesamtgröße bzw. Anzahl unter den Spalten Größe und Dateiname ausgerichtet.

---

## [1.0.0.25] – 2026-02-02

### Geändert

- **Status Backup-Liste**: In der Backup-Liste (--status) werden jetzt in einer zweiten Spalte die Zeit der Dateierstellung (ModTime) und in einer dritten Spalte die Größe angezeigt. Größen: Bytes ohne Endung; 1024·n als „nK“, 1024²·n als „nM“, 1024³·n als „nT“; eine Nachkommastelle wenn die Zahl < 10 ist, sonst keine. Abschließend eine Zeile „Summe: \<Gesamtgröße\>  \<Anzahl\> Datei(en)“.

---

## [1.0.0.24] – 2026-02-02

### Hinzugefügt

- **Remote-Verschlüsselung (AES-256-CTR)**: Dateien können vor dem Upload zum Remote-Server mit AES-256-CTR verschlüsselt werden. Schlüssel wird aus `remote_aes_password` abgeleitet (PBKDF2, 100.000 Iterationen). Wenn der entschlüsselte Wert leer ist, erfolgt keine Verschlüsselung. Streaming: Verschlüsselung beim Lesen/Schreiben, kein voller Inhalt im Speicher. Remote-Dateien behalten den Namen (z. B. `.zip`), Inhalt ist salt+nonce+ciphertext.
- **Remote-Sync statt Copy**: Remote-Verzeichnis wird mit lokalem Backup-Verzeichnis synchron gehalten: zuerst werden vorhandene Remote-Dateien (Name, Änderungsdatum, Größe) und lokale Backup-ZIPs ermittelt. Lokale Datei wird hochgeladen (ggf. verschlüsselt), wenn sie remote fehlt oder neuer/größer ist. Remote-Dateien, die lokal nicht mehr existieren, werden auf dem Remote-Server gelöscht.

### Geändert

- **Config**: Neue Optionen `remote_aes_password` und `remote_aes_secure_password` (sconfig-kompatibel) für optionale AES-Verschlüsselung beim Remote-Upload.
- **Run**: Nach dem Backup wird `remote.Sync` statt einer einfachen Copy aufgerufen.

---

## [1.0.0.23] – 2026-02-02

### Hinzugefügt

- **Startup-Log**: Beim Start werden Aufrufpfad (Executable), Versionsnummer und Aufrufparameter ins Log geschrieben (bei --init, --backup, --remove sobald ein Logger angelegt wurde).

---

## [1.0.0.22] – 2026-02-02

### Geändert

- **Streaming statt voller Dump im Speicher**: Der mysqldump-Output wird nicht mehr komplett im RAM gehalten. `DumpDatabase` schreibt direkt in einen `io.Writer` (mysqldump stdout → dest). Beim Backup: ZIP wird per `safeWriteZIPStreaming` geöffnet, Dump wird in den ZIP-Eintrag gestreamt, danach wird der User-Block angehängt, dann ZIP geschlossen. So können auch sehr große Dumps gesichert werden, ohne den verfügbaren Hauptspeicher zu überschreiten. `safeWriteZIP`/`writeZIP` durch `safeWriteZIPStreaming` ersetzt.

---

## [1.0.0.21] – 2026-02-02

### Geändert

- **User-Namen aus gleicher Struktur**: Die Nutzernamen fürs Logging kommen nicht mehr aus einem zweiten Parsing. `ParseUserSQL` gibt jetzt `(dbToSQL, userNames)` zurück; `userNames` wird aus derselben geparsten User-Struktur erzeugt (`userNamesFromUsers`). `UserNamesFromSQL` wurde entfernt; in `backup.Run` werden die Namen direkt aus dem zweiten Rückgabewert von `ParseUserSQL` verwendet.

---

## [1.0.0.20] – 2026-02-02

### Geändert

- **MariaDB 10 + Unicode BMP**: Die Regeln für unquoted Identifikatoren gelten gleich für MySQL 8 und MariaDB 10 (laut Doku). Die unquoted-Pattern in userHostRe und grantOnDbRe erlauben jetzt zusätzlich Unicode BMP U+0080..U+FFFF (`[a-zA-Z0-9$_\x{80}-\x{FFFF}]+`). Paket-Kommentar entsprechend ergänzt.

---

## [1.0.0.19] – 2026-02-02

### Geändert

- **IDENTIFIED BY PASSWORD und ON db.\***: IDENTIFIED BY PASSWORD unterstützt jetzt ein beliebiges Quote (`` `...` ``, `"..."`, `'...'`) mit passendem schließendem Zeichen. Der Datenbankname nach ON (vor `.*`) unterstützt dieselben vier Formen wie User/Host (`` `db` ``, `"db"`, `'db'`, unquoted). Beim Strippen von IDENTIFIED BY in GRANT-Zeilen werden alle drei Quote-Typen erkannt.

---

## [1.0.0.18] – 2026-02-02

### Geändert

- **User/Host-Regex**: Erkennung von User/Host unterstützt alle vier Formen mit passenden Anführungszeichen: `` `name` ``, `"name"`, `'name'` und unquoted `name`. Das Regex prüft, dass öffnendes und schließendes Anführungszeichen übereinstimmen; der Inhalt wird einheitlich als `name` ausgewertet.

---

## [1.0.0.17] – 2026-02-02

### Geändert

- **User/Host-Regex**: Erkennung von `'user'@'host'` setzt voraus, dass Namen keine Anführungszeichen enthalten (keine `'`, `"`, `` ` ``). Das Regex wurde entsprechend vereinfacht.

---

## [1.0.0.16] – 2026-02-02

### Geändert

- **User-Parsing neu aufgebaut**: Die Auswertung des User-Dumps (CREATE USER / GRANT) erfolgt jetzt musterbasierbar. Pro User werden erfasst: Name, Liste der Hosts (aus CREATE USER und GRANT … TO name@host), Passwort-Hash (erstes; bei unterschiedlichen Hashes pro Host wird eine Warnung ausgegeben), GRANT-Statements sowie die Liste der Datenbanken (aus GRANT … ON db.*). Ausgabe pro Backup-DB: CREATE USER IF NOT EXISTS für jeden Host (mit IDENTIFIED BY PASSWORD), danach alle GRANT-Zeilen für diese DB ohne IDENTIFIED-BY-Zusatz. `ParseUserSQL` hat einen optionalen Warn-Callback (z. B. `log.Warn`).

---

## [1.0.0.15] – 2026-02-02

### Geändert

- **MariaDB User-Export ohne --system=users**: Viele MariaDB-Versionen (z. B. auf Synology/DSM) kennen die Option `--system=users` von mysqldump nicht. Wenn der Aufruf mit „unknown“/„unrecognized“/„unknown variable“ fehlschlägt, wird automatisch ein Fallback genutzt: User werden per `mysql.user` und `SHOW GRANTS FOR 'user'@'host'` exportiert. Das Ergebnis (CREATE USER + GRANT-Zeilen) entspricht dem erwarteten Format für die Backup-Anhänge.

---

## [1.0.0.14] – 2026-02-02

### Geändert

- **Standard-Config-Dateiname**: Die Standard-Config-Datei heißt nun `config.json` (statt `mysqlbackup_config.json`). Die Beispiel-Config heißt `config.example.json` (statt `mysqlbackup_config.example.json`). Angepasst im Code (`internal/config/config.go`), in `.vscode/launch.json`, in README/README.de.md und im Changelog; die Datei `mysqlbackup_config.example.json` wurde in `config.example.json` umbenannt.

---

## [1.0.0.13] – 2026-02-02

### Geändert

- **sys-Datenbank übersprungen**: Die System-Datenbank `sys` (MySQL 5.7+) wird beim Auflisten der zu sichernden Datenbanken nicht mehr einbezogen (wie bereits `information_schema`, `performance_schema`, `mysql`). Sie enthält keine Anwendungsdaten und wird vom Server bereitgestellt.

---

## [1.0.0.12] – 2026-02-02

### Hinzugefügt

- **User-Logging**: Beim Backup werden die gefundenen User (aus dem User-Export) geloggt (Anzahl und Liste z. B. „u1@%, u2@localhost“).

### Geändert

- **Sicheres Überschreiben von Backupdateien**: Bestehende ZIP-Dateien werden nicht mehr direkt überschrieben. Vor dem Schreiben wird die bestehende Datei in *.sav umbenannt. Nach erfolgreichem Schreiben der neuen Datei wird die .sav gelöscht. Schlägt das Schreiben fehl, wird die fehlerhafte neue Datei gelöscht und die .sav zurück in .zip umbenannt.
- **Startup: Aufräumen von .sav**: Beim Start eines Backups werden vorhandene *.sav-Dateien im Backup-Verzeichnis behandelt: Existieren sowohl .zip als auch .sav mit gleichem Basisnamen, wird die größere Datei behalten (die andere gelöscht, ggf. .sav in .zip umbenannt). Existiert nur die .sav, wird sie in .zip umbenannt.

---

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

- MySQL-Lifecycle (z. B. XAMPP): Prüfung, ob MySQL läuft; wenn nicht und `mysql_auto_start_stop` aktiv, Start mit `mysql_start_cmd`, nach Backup Stopp mit `mysql_stop_cmd`. Config: `mysql_auto_start_stop`, `mysql_start_cmd`, `mysql_stop_cmd`. Lokale `config.json` für XAMPP mit typischen Pfaden (`C:\xampp\mysql_start.bat` / `mysql_stop.bat`).

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
