# Split-Lock-Erkennung

TitanOS verwendet die aktive Split-Lock-Erkennung des Kernels im Standardmodus
`warn`. Es setzt weder einen abschaltenden Bootparameter noch
`kernel.split_lock_mitigate=0`. Boot-Einträge werden dafür nicht automatisch
umgeschrieben. Die zwischenzeitlich gewünschte NAS-weite Abschaltung wurde auf
erneute Entscheidung des Projektinhabers vor Veröffentlichung zurückgenommen.
Das produktive NAS wurde dabei nicht verändert.

Die Meldung `x86/split lock detection: #AC: bambustu_main … took a split_lock
trap` bleibt als Warnung sichtbar. Ein entsprechender Fehler ist bei der
Netzwerkbibliothek von [Bambu Studio dokumentiert](https://github.com/bambulab/BambuStudio/issues/8605).
Die [Kernel-Dokumentation](https://www.kernel.org/doc/html/latest/arch/x86/buslock.html)
beschreibt die Erkennung und Drosselung solcher Speicherzugriffe. TitanOS
verändert historische Meldungen nicht.
