# -*- coding: utf-8 -*-
"""トレイ用アイコンを PIL なしで生成する。

意匠: Claude 風スターバースト（＊）から音波 )) が出る「読み上げ」アイコン。色はオレンジ。
- 透過背景（塗りブロックを廃止 → 他のトレイアイコンと馴染む）
- スーパーサンプリングで擬似アンチエイリアス（依存ゼロ）
- Windows 用 .ico は 16/20/24/32/48 のマルチ解像度を内包
- Linux 用 .png は 48px 透過
- 状態:
    on       = オレンジの ＊ + 音波 ))         （有効・待機）
    off      = グレーの ＊ + 音波 + ミュート斜線（無効）
    speaking = オレンジの ＊ + 停止■            （発話中・クリックで停止）

確認用に temp/preview_*.png（128px）も書き出す。
"""
import struct
import os
import zlib
from math import atan2, hypot, pi

# ---- 色 -------------------------------------------------------------
ORANGE = (233, 124, 60)     # #E97C3C 通常時のマーク/音波
ORANGE_ACT = (231, 98, 46)  # #E7622E 発話中(やや赤寄り=停止の合図)
GRAY = (140, 148, 158)      # #8C949E 無効時のマーク/音波
SLASH = (70, 76, 86)        # ミュート斜線
RED = (224, 49, 49)         # #E03131 発話中の停止ボタン(赤)

# ---- 形状パラメータ（単位座標 0..1, y下向き）-----------------------
CX, CY = 0.38, 0.50         # スターバースト中心（右に音波を置くため左寄せ）
R = 0.33                    # スターバースト最大半径
SPIKES = 9                  # 放射数（少なめ＝太く、16pxでも潰れない）
CORE = 0.32                 # 中心の塗り（R正規化）太め
W0 = (pi / SPIKES) * 0.95   # 各スパイクの中心角半幅（太め）

WAVES = ((0.44, 0.095), (0.58, 0.095))  # (半径, 太さ)太い2本
WAVE_ANGLE = 0.66           # 音波の開き角(±rad)

SQ_CX, SQ_HALF, SQ_R = 0.72, 0.17, 0.05   # 停止四角(中心x, 半幅, 角丸)
SLASH_A, SLASH_B = (0.17, 0.17), (0.83, 0.83)
SLASH_T, SLASH_GAP = 0.13, 0.05

SS = 8  # スーパーサンプリング数(1辺)


def _in_starburst(x, y):
	dx, dy = x - CX, y - CY
	d = hypot(dx, dy)
	r = d / R
	if r > 1.0:
		return False
	if r <= CORE:
		return True
	ang = atan2(dy, dx)
	step = 2 * pi / SPIKES
	k = round(ang / step)               # 最寄りスパイク
	da = abs(ang - k * step)
	return da <= W0 * (1.0 - r)


def _in_waves(x, y):
	dx, dy = x - CX, y - CY
	if dx <= 0.02:
		return False
	if abs(atan2(dy, dx)) > WAVE_ANGLE:
		return False
	d = hypot(dx, dy)
	for rw, t in WAVES:
		if abs(d - rw) <= t / 2:
			return True
	return False


def _in_square(x, y):
	dx, dy = abs(x - SQ_CX), abs(y - CY)
	if dx > SQ_HALF or dy > SQ_HALF:
		return False
	ix, iy = SQ_HALF - SQ_R, SQ_HALF - SQ_R   # 角丸
	if dx > ix and dy > iy:
		return hypot(dx - ix, dy - iy) <= SQ_R
	return True


def _in_rounded(x, y, cx, cy, half, rad):
	"""中心(cx,cy)・半サイズhalf・角丸radの角丸四角の内側か。"""
	dx, dy = abs(x - cx), abs(y - cy)
	if dx > half or dy > half:
		return False
	ix, iy = half - rad, half - rad
	if dx > ix and dy > iy:
		return hypot(dx - ix, dy - iy) <= rad
	return True


def _dist_to_seg(x, y, a, b):
	ax, ay = a
	bx, by = b
	vx, vy = bx - ax, by - ay
	wx, wy = x - ax, y - ay
	t = (wx * vx + wy * vy) / (vx * vx + vy * vy)
	t = max(0.0, min(1.0, t))
	return hypot(x - (ax + t * vx), y - (ay + t * vy))


def _sample(x, y, kind):
	"""単位座標(x,y)の色 (r,g,b,a)。透過は a=0。"""
	if kind == "on":
		if _in_starburst(x, y) or _in_waves(x, y):
			return (*ORANGE, 255)
		return (0, 0, 0, 0)
	if kind == "speaking":
		# 赤い角丸の停止ボタン + 白い停止マーク(■)。クリックで停止の合図。
		if _in_rounded(x, y, 0.5, 0.5, 0.42, 0.12):
			if _in_rounded(x, y, 0.5, 0.5, 0.20, 0.03):
				return (245, 245, 245, 255)
			return (*RED, 255)
		return (0, 0, 0, 0)
	# off: グレーのマーク+音波 → 斜線(芯)とギャップ(透明)で上書き
	d = _dist_to_seg(x, y, SLASH_A, SLASH_B)
	if d <= SLASH_T / 2:
		return (*SLASH, 255)
	base = _in_starburst(x, y) or _in_waves(x, y)
	if d <= SLASH_T / 2 + SLASH_GAP:
		return (0, 0, 0, 0)   # 斜線の周囲を切り抜いてミュートを強調
	if base:
		return (*GRAY, 255)
	return (0, 0, 0, 0)


def render(size, kind):
	"""size×size の RGBA ピクセル(上→下)。被覆平均でAA。"""
	px = [[(0, 0, 0, 0)] * size for _ in range(size)]
	inv = 1.0 / size
	n = SS * SS
	for py in range(size):
		row = px[py]
		for pxX in range(size):
			spr = spg = spb = sa = 0
			for j in range(SS):
				uy = (py + (j + 0.5) / SS) * inv
				for i in range(SS):
					ux = (pxX + (i + 0.5) / SS) * inv
					r, g, b, a = _sample(ux, uy, kind)
					if a:
						spr += r
						spg += g
						spb += b
						sa += 1
			if sa == 0:
				row[pxX] = (0, 0, 0, 0)
			else:
				alpha = int(round(255 * sa / n))
				row[pxX] = (spr // sa, spg // sa, spb // sa, alpha)
	return px


# ---- 書き出し -------------------------------------------------------
def _bmp_image(px):
	size = len(px)
	bih = struct.pack('<IiiHHIIiiII', 40, size, size * 2, 1, 32, 0, 0, 0, 0, 0, 0)
	rows = []
	for y in range(size - 1, -1, -1):        # ボトムアップ
		row = bytearray()
		for x in range(size):
			r, g, b, a = px[y][x]
			row += bytes((b, g, r, a))        # BGRA
		rows.append(bytes(row))
	xor = b''.join(rows)
	mask_row = ((size + 31) // 32) * 4
	andmask = b'\x00' * (mask_row * size)
	return bih + xor + andmask


def write_ico(path, images):
	"""images: [px,...] 複数解像度を 1 つの .ico に。"""
	blobs = [_bmp_image(p) for p in images]
	n = len(blobs)
	out = bytearray(struct.pack('<HHH', 0, 1, n))
	offset = 6 + 16 * n
	entries = bytearray()
	for p, blob in zip(images, blobs):
		s = len(p)
		entries += struct.pack('<BBBBHHII', s if s < 256 else 0, s if s < 256 else 0,
								0, 0, 1, 32, len(blob), offset)
		offset += len(blob)
	out += entries
	for blob in blobs:
		out += blob
	with open(path, 'wb') as f:
		f.write(out)
	print('wrote', path, len(out), 'bytes', [len(p) for p in images])


def write_png(path, px):
	size = len(px)

	def chunk(typ, data):
		body = typ + data
		return struct.pack('>I', len(data)) + body + struct.pack('>I', zlib.crc32(body) & 0xffffffff)

	raw = bytearray()
	for y in range(size):
		raw.append(0)
		for x in range(size):
			r, g, b, a = px[y][x]
			raw += bytes((r, g, b, a))
	sig = b'\x89PNG\r\n\x1a\n'
	ihdr = struct.pack('>IIBBBBB', size, size, 8, 6, 0, 0, 0)
	idat = zlib.compress(bytes(raw), 9)
	with open(path, 'wb') as f:
		f.write(sig + chunk(b'IHDR', ihdr) + chunk(b'IDAT', idat) + chunk(b'IEND', b''))
	print('wrote', path)


ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ASSETS = os.path.join(ROOT, 'assets')
TEMP = os.path.join(ROOT, 'temp')
os.makedirs(ASSETS, exist_ok=True)
os.makedirs(TEMP, exist_ok=True)

ICO_SIZES = [16, 20, 24, 32, 48]
for kind in ('on', 'off', 'speaking'):
	imgs = [render(s, kind) for s in ICO_SIZES]
	write_ico(os.path.join(ASSETS, f'icon_{kind}.ico'), imgs)              # Windows: マルチ解像度
	write_png(os.path.join(ASSETS, f'icon_{kind}.png'), render(48, kind))  # Linux
	write_png(os.path.join(TEMP, f'preview_{kind}.png'), render(128, kind))  # 確認用
print('done')
