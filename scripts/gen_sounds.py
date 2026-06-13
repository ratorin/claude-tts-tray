# -*- coding: utf-8 -*-
# 埋め込み用の短い効果音WAVを生成する(合成エンジン不要の既定音)。
# sound_done.wav   : 応答完了(Stop)の「タラッ」上昇2音
# sound_notify.wav : 確認(Notification)の単音「ピロン」
import struct
import math
import os

SR = 44100


def tone(freq, dur, vol=0.5, decay=True):
	n = int(SR * dur)
	out = []
	for i in range(n):
		t = i / SR
		env = math.exp(-3.5 * t / dur) if decay else 1.0
		# フェードイン(クリック防止)
		fin = min(1.0, i / (SR * 0.005))
		# 基音 + 軽い倍音でベル感
		s = math.sin(2 * math.pi * freq * t) + 0.35 * math.sin(2 * math.pi * 2 * freq * t)
		out.append(s * vol * env * fin / 1.35)
	return out


def silence(dur):
	return [0.0] * int(SR * dur)


def write_wav(path, samples):
	data = bytearray()
	for s in samples:
		v = int(max(-1.0, min(1.0, s)) * 32767)
		data += struct.pack('<h', v)
	with open(path, 'wb') as f:
		f.write(b'RIFF')
		f.write(struct.pack('<I', 36 + len(data)))
		f.write(b'WAVE')
		f.write(b'fmt ')
		f.write(struct.pack('<IHHIIHH', 16, 1, 1, SR, SR * 2, 2, 16))
		f.write(b'data')
		f.write(struct.pack('<I', len(data)))
		f.write(data)
	print('wrote', path, len(data) + 44, 'bytes')


here = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'assets')
os.makedirs(here, exist_ok=True)
# 完了音: C5 -> G5 の上昇2音
done = tone(523.25, 0.13) + silence(0.02) + tone(783.99, 0.30)
write_wav(os.path.join(here, 'sound_done.wav'), done)
# 確認音: A5 単音(やや長めの余韻)
notify = tone(880.0, 0.33)
write_wav(os.path.join(here, 'sound_notify.wav'), notify)
