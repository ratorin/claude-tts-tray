# -*- coding: utf-8 -*-
"""トレイ用 .ico を PIL なしで生成する。
32x32 32bpp BGRA のシンプルな「スピーカー」アイコンを ON(緑)/OFF(灰) の2色で出力。
"""
import struct
import os

W = H = 32

def make_pixels(accent):
	"""accent=(r,g,b) のスピーカー風アイコンのBGRAピクセル(上から下)を返す。"""
	ar, ag, ab = accent
	px = [[(0, 0, 0, 0) for _ in range(W)] for _ in range(H)]

	def setp(x, y, rgba):
		if 0 <= x < W and 0 <= y < H:
			px[y][x] = rgba

	# 角丸の濃紺ベース
	base = (30, 34, 48, 255)
	for y in range(H):
		for x in range(W):
			# 角を少し落とす
			cx = min(x, W - 1 - x)
			cy = min(y, H - 1 - y)
			if cx + cy >= 3:
				setp(x, y, base)

	# スピーカー本体（白）: 左の四角＋右に開く三角
	white = (235, 238, 245, 255)
	# 四角部分
	for y in range(13, 19):
		for x in range(8, 12):
			setp(x, y, white)
	# 三角コーン部分
	for y in range(8, 24):
		half = abs(y - 15)
		x_start = 12
		x_end = 12 + (8 - half // 1)
		for x in range(x_start, max(x_start, x_end)):
			if (x - 12) <= (8 - half):
				setp(x, y, white)

	# 音波（アクセント色）= ON/OFF を色で表現
	wave = (ar, ag, ab, 255)
	for y in range(11, 21):
		for x in range(22, 24):
			if 12 <= y <= 19:
				setp(x, y, wave)
	for y in range(8, 24):
		for x in range(25, 27):
			if 9 <= y <= 22:
				setp(x, y, wave)
	return px


def write_ico(path, px):
	# BITMAPINFOHEADER (height は XOR+AND マスク分で2倍)
	bih = struct.pack('<IiiHHIIiiII', 40, W, H * 2, 1, 32, 0, 0, 0, 0, 0, 0)
	# ピクセルは BGRA・ボトムアップ
	rows = []
	for y in range(H - 1, -1, -1):
		row = bytearray()
		for x in range(W):
			r, g, b, a = px[y][x]
			row += bytes((b, g, r, a))
		rows.append(bytes(row))
	xor = b''.join(rows)
	# AND マスク: 1bpp, 行は32bit境界padding, 全0（アルファ使用）
	mask_row_bytes = ((W + 31) // 32) * 4
	andmask = b'\x00' * (mask_row_bytes * H)
	img = bih + xor + andmask

	# ICONDIR + ICONDIRENTRY
	icondir = struct.pack('<HHH', 0, 1, 1)
	offset = 6 + 16
	entry = struct.pack('<BBBBHHII', W if W < 256 else 0, H if H < 256 else 0,
						 0, 0, 1, 32, len(img), offset)
	with open(path, 'wb') as f:
		f.write(icondir + entry + img)
	print('wrote', path, len(icondir + entry + img), 'bytes')


def make_stop_pixels():
	"""発話中アイコン: オレンジの角丸ベース + 白い停止四角(■)。"""
	base = (232, 115, 44, 255)  # オレンジ
	px = [[(0, 0, 0, 0) for _ in range(W)] for _ in range(H)]

	def setp(x, y, c):
		if 0 <= x < W and 0 <= y < H:
			px[y][x] = c

	for y in range(H):
		for x in range(W):
			cx = min(x, W - 1 - x)
			cy = min(y, H - 1 - y)
			if cx + cy >= 3:
				setp(x, y, base)
	white = (245, 245, 245, 255)
	for y in range(10, 22):
		for x in range(10, 22):
			setp(x, y, white)
	return px


here = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
write_ico(os.path.join(here, 'icon_on.ico'), make_pixels((90, 210, 120)))   # 緑=有効
write_ico(os.path.join(here, 'icon_off.ico'), make_pixels((120, 128, 140)))  # 灰=無効
write_ico(os.path.join(here, 'icon_speaking.ico'), make_stop_pixels())       # オレンジ=発話中(クリックで停止)
