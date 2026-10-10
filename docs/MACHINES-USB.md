# USB-Geräte an virtuelle Maschinen durchreichen

Unter **Virtuelle Maschinen → Maschine → Einstellungen → USB-Geräte** stehen die
am Titan angeschlossenen USB-Geräte. Ein Schalter reicht das Gerät an die
Maschine durch, zum Beispiel einen Zigbee-, Z-Wave- oder Bluetooth-Stick für
Home Assistant.

- Die Änderung wirkt sofort, auch bei laufender Maschine.
- Ein durchgereichtes Gerät steht Titan und anderen Maschinen nicht mehr zur
  Verfügung. Jedes Gerät kann nur einer Maschine zugewiesen sein.
- Titan merkt sich das Gerät anhand von Hersteller, Produkt und Seriennummer.
  Nach einem Neustart oder nach dem Aus- und Einstecken wird es innerhalb
  weniger Sekunden wieder verbunden. Ist es beim Start der Maschine nicht
  angeschlossen, startet die Maschine ohne das Gerät.
- Baugleiche Geräte ohne Seriennummer lassen sich nicht unterscheiden; Titan
  weist dann das zuerst gefundene zu.
- USB-Hubs und USB-Speicher werden nicht angeboten. Speicher verwaltet Titan
  unter **Dateien**.
- Pro Maschine sind bis zu acht USB-Geräte möglich.

Grafikkarten, NPUs und andere PCI-Geräte werden damit nicht durchgereicht.
