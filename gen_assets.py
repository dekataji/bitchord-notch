# Generates icon.ico (multi-size, PNG entries) + app.manifest for the
# portable build. Pure stdlib; run once, outputs are committed.
import struct, zlib

AMBER = (255, 179, 0, 255)
DARK = (16, 18, 20, 255)
CLEAR = (0, 0, 0, 0)

def render(n):
    # Draw in a 48-unit space, sampled per output pixel.
    px = [[CLEAR] * n for _ in range(n)]
    def at(u, v):  # u,v in 0..48 space
        # rounded-square backdrop, radius 10
        x, y = u, v
        r = 10
        dx = max(r - x, 0, x - (48 - r))
        dy = max(r - y, 0, y - (48 - r))
        if dx * dx + dy * dy > r * r and (dx or dy):
            return CLEAR
        if not (0 <= x < 48 and 0 <= y < 48):
            return CLEAR
        c = DARK
        # note stem: x 30..34, y 9..34
        if 30 <= x < 34 and 9 <= y < 34:
            c = AMBER
        # note flag: triangle (34,9) (43,15) (34,21)
        if x >= 34:
            t = (x - 34) / 9.0
            if 9 + 6 * t - 1 <= y <= 21 - 6 * t + 6 * t * t:
                if y <= 9 + 12 * t - 3 * t * t:
                    c = AMBER
        # note head: ellipse centre (25,35) rx 8 ry 6
        if ((x - 25) / 8.0) ** 2 + ((y - 35) / 6.0) ** 2 <= 1:
            c = AMBER
        # second head: centre (37,38) rx 6 ry 4.8, stem x 42..45 y 12..38
        if 42 <= x < 45 and 12 <= y < 38:
            c = AMBER
        if ((x - 37) / 6.0) ** 2 + ((y - 38) / 4.8) ** 2 <= 1:
            c = AMBER
        return c
    for j in range(n):
        for i in range(n):
            # 2x2 supersample for smoother edges
            acc = [0, 0, 0, 0]
            for sy in (0.25, 0.75):
                for sx in (0.25, 0.75):
                    c = at((i + sx) * 48 / n, (j + sy) * 48 / n)
                    for k in range(4):
                        acc[k] += c[k]
            px[j][i] = tuple(a // 4 for a in acc)
    return px

def png(px):
    n = len(px)
    raw = b""
    for row in px:
        raw += b"\x00" + b"".join(struct.pack("BBBB", *p) for p in row)
    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    ihdr = struct.pack(">IIBBBBB", n, n, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) +
            chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))

sizes = [48, 32, 16]
imgs = [png(render(s)) for s in sizes]
hdr = struct.pack("<HHH", 0, 1, len(sizes))
off = 6 + 16 * len(sizes)
entries = b""
for s, img in zip(sizes, imgs):
    entries += struct.pack("<BBBBHHII", s if s < 256 else 0, s if s < 256 else 0,
                           0, 0, 1, 32, len(img), off)
    off += len(img)
open("icon.ico", "wb").write(hdr + entries + b"".join(imgs))
print("icon.ico", sum(len(i) for i in imgs) + off - sum(len(i) for i in imgs), "bytes total")
