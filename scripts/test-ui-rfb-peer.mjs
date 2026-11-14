// A synthetic RFB 3.8 peer for the UI tests of the VM screen (PLAN 87): it
// answers noVNC's handshake, paints a test picture and records what noVNC
// sends – pointer events (button mask and place) and key events (keysym,
// down/up). Runs in the page (ctx.addInitScript(rfbPeer)); only the NAS's
// own sockets get the peer, Vite's HMR socket stays real. Never connects to a
// NAS or a real VM.
export function rfbPeer() {
  const fixture = {
    latest: null,
    keys: 0,
    connections: 0,
    /** [{ mask, x, y }] in the guest's pixels. */
    pointer: [],
    /** [{ keysym, down }] */
    keyEvents: [],
  };
  window.__vncFixture = fixture;
  class RfbPeer {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    constructor(url) {
      this.url = url;
      this.binaryType = "arraybuffer";
      this.protocol = "";
      this.readyState = 0;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.onclose = null;
      this.step = 0;
      this.width = 640;
      this.height = 360;
      this.painted = false;
      fixture.latest = this;
      fixture.connections++;
      setTimeout(() => {
        if (this.readyState !== 0) return;
        this.readyState = 1;
        this.onopen?.({});
        this.emit(new TextEncoder().encode("RFB 003.008\n"));
      }, 0);
    }
    emit(bytes) {
      setTimeout(() => {
        if (this.readyState === 1)
          this.onmessage?.({
            data: bytes.buffer.slice(
              bytes.byteOffset,
              bytes.byteOffset + bytes.byteLength,
            ),
          });
      }, 0);
    }
    send(data) {
      const bytes =
        data instanceof ArrayBuffer
          ? new Uint8Array(data)
          : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
      if (this.step === 0) {
        this.step++;
        this.emit(new Uint8Array([1, 1]));
        return;
      }
      if (this.step === 1) {
        this.step++;
        this.emit(new Uint8Array(4));
        return;
      }
      if (this.step === 2) {
        this.step++;
        const name = new TextEncoder().encode("Synthetic VNC test guest");
        const init = new Uint8Array(24 + name.length),
          view = new DataView(init.buffer);
        view.setUint16(0, this.width);
        view.setUint16(2, this.height);
        init[4] = 32;
        init[5] = 24;
        init[7] = 1;
        view.setUint16(8, 255);
        view.setUint16(10, 255);
        view.setUint16(12, 255);
        init[14] = 16;
        init[15] = 8;
        view.setUint32(20, name.length);
        init.set(name, 24);
        this.emit(init);
        return;
      }
      // Several client messages may come in one send: walk through them.
      let i = 0;
      while (i < bytes.length) {
        const type = bytes[i];
        const view = new DataView(bytes.buffer, bytes.byteOffset + i);
        if (type === 4) {
          fixture.keys++;
          fixture.keyEvents.push({
            down: bytes[i + 1] === 1,
            keysym: view.getUint32(4),
          });
          i += 8;
        } else if (type === 5) {
          fixture.pointer.push({
            mask: bytes[i + 1],
            x: view.getUint16(2),
            y: view.getUint16(4),
          });
          i += 6;
        } else if (type === 3) {
          if (!this.painted) {
            this.painted = true;
            this.paint(false);
          }
          i += 10;
        } else if (type === 0) i += 20;
        else if (type === 2) i += 4 + 4 * view.getUint16(2);
        else break;
      }
    }
    paint(resize) {
      const canvas = document.createElement("canvas");
      canvas.width = this.width;
      canvas.height = this.height;
      const ctx = canvas.getContext("2d");
      ctx.fillStyle = "#0b2140";
      ctx.fillRect(0, 0, this.width, this.height);
      ctx.fillStyle = "#16325c";
      ctx.fillRect(0, this.height - 28, this.width, 28);
      ctx.fillStyle = "#d8dee7";
      ctx.font = "14px monospace";
      ctx.fillText("Debian GNU/Linux 13", 24, 42);
      ctx.fillText("VNC-Testgast · isolierte Beispieldaten", 24, 78);
      ctx.fillStyle = "#8ac6a6";
      ctx.fillText("alex@debian:~$ _", 24, 132);
      ctx.fillStyle = "#e8edf4";
      ctx.fillRect(24, 170, 180, 110);
      ctx.fillStyle = "#1d3b66";
      ctx.fillText("Dateien", 40, 200);
      const rgba = ctx.getImageData(0, 0, this.width, this.height).data;
      const prefix = resize ? 28 : 16;
      const update = new Uint8Array(prefix + rgba.length),
        view = new DataView(update.buffer);
      view.setUint16(2, resize ? 2 : 1);
      if (resize) {
        view.setUint16(8, this.width);
        view.setUint16(10, this.height);
        view.setInt32(12, -223);
      }
      const rectangle = resize ? 16 : 4;
      view.setUint16(rectangle + 4, this.width);
      view.setUint16(rectangle + 6, this.height);
      // noVNC asks for red in the lowest byte (SetPixelFormat).
      for (let i = 0; i < rgba.length; i += 4) {
        update[prefix + i] = rgba[i];
        update[prefix + i + 1] = rgba[i + 1];
        update[prefix + i + 2] = rgba[i + 2];
      }
      this.emit(update);
    }
    resize(width, height) {
      this.width = width;
      this.height = height;
      this.paint(true);
    }
    disconnect() {
      if (this.readyState === 3) return;
      this.readyState = 3;
      this.onclose?.({
        code: 1006,
        reason: "Synthetic disconnect",
        wasClean: false,
      });
    }
    close() {
      this.readyState = 3;
    }
  }
  const Native = window.WebSocket;
  function Socket(url, protocols) {
    return /^vite-hmr/.test(String(protocols ?? ""))
      ? new Native(url, protocols)
      : new RfbPeer(url);
  }
  Object.assign(Socket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  window.WebSocket = Socket;
}
