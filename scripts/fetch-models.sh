#!/bin/sh
# Fills <dir> with module ai's model files, each checked against its sha256:
# Gipformer 1.5 and Silero VAD for speech, SigLIP 2 in fp32 for photo vectors.
#   fetch-models.sh <dir>
# It runs on every start of a stack (compose's fetch service, the chart's
# init container), so it downloads only a file that is missing or whose
# sum is wrong, to a .part file renamed once its sum matches. A sum that
# still does not match fails the run. FETCH_EMBED=0 skips SigLIP 2, for a
# stack whose AI_EMBED_MODELS is empty. The image ships it as /fetch-models.sh.
set -eu
dir=${1:?usage: fetch-models.sh <dir>}

GIPFORMER=https://huggingface.co/g-group-ai-lab/gipformer1.5-68M-rnnt/resolve/dd9227dcd8705c13f33bdbe59728d546ab94480f
SILERO=https://github.com/k2-fsa/sherpa-onnx/releases/download/asr-models/silero_vad.onnx
SIGLIP=https://huggingface.co/onnx-community/siglip2-so400m-patch16-384-ONNX/resolve/29f74e673c298fd07482d8e20138179282001eb7

# fetch SUM PATH URL puts URL at <dir>/PATH unless a file with SUM is there.
fetch() {
	path=$dir/$2
	if [ -f "$path" ] && printf '%s  %s\n' "$1" "$path" | sha256sum -c --status -; then
		echo "have $2"
		return
	fi
	echo "fetch $2"
	mkdir -p "$(dirname "$path")"
	curl -fsSL --retry 3 -o "$path.part" "$3"
	if ! printf '%s  %s\n' "$1" "$path.part" | sha256sum -c --status -; then
		rm -f "$path.part"
		echo "fetch-models: $2 does not match its sha256" >&2
		exit 1
	fi
	mv "$path.part" "$path"
}

fetch 96b3c83c9c39afc4421f8fd2914d26b53ae837e56788a7bd5aece61580937c36 gipformer-1.5/encoder.onnx "$GIPFORMER/encoder.onnx"
fetch ea584ee86d6f74191f934f8b950013adcb2c33483785aa8b1b44fff3a7b30069 gipformer-1.5/decoder.onnx "$GIPFORMER/decoder.onnx"
fetch d146a1cff4a0060d7031e5eb91295773661bc52c356c952dd1e2759e60ffa94d gipformer-1.5/joiner.onnx "$GIPFORMER/joiner.onnx"
fetch f536d03c2e95ebd2930cf0abec88e823bd17d3c1933da7ae6a82db3b80605e15 gipformer-1.5/tokens.txt "$GIPFORMER/tokens.txt"
fetch 9e2449e1087496d8d4caba907f23e0bd3f78d91fa552479bb9c23ac09cbb1fd6 silero_vad.onnx "$SILERO"

if [ "${FETCH_EMBED:-1}" = 0 ]; then
	echo "skip siglip2-so400m-patch16-384 (FETCH_EMBED=0)"
	exit 0
fi
# SigLIP 2 in fp32: the int8 files lose too much.
s=siglip2-so400m-patch16-384
fetch e3f730b2a37c69ac08c2159fe3c7fab97f23fbe3f32319f9953375ba82ecb1e6 $s/vision_model.onnx "$SIGLIP/onnx/vision_model.onnx"
fetch 1be6a35f94aa0d1aa15024569ccb7637c7157809624922e50e22d746d43745a5 $s/text_model.onnx "$SIGLIP/onnx/text_model.onnx"
fetch 80d50b22c6a56758717e6acdbf03f3e564b3a701d152b6741d8b62181cae7983 $s/text_model.onnx_data "$SIGLIP/onnx/text_model.onnx_data"
fetch 61a7b147390c64585d6c3543dd6fc636906c9af3865a5548f27f31aee1d4c8e2 $s/tokenizer.model "$SIGLIP/tokenizer.model"
