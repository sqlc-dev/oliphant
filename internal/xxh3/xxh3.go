// Package xxh3 is an in-house, pure Go implementation of the XXH3 hash
// function's 64-bit variant (XXH3_64bits_withSeed), ported from the xxHash
// reference implementation vendored by libpg_query (xxhash.h, v0.8.x,
// BSD-2-Clause, (c) Yann Collet).
//
// It exists because pg_query_go exports HashXXH3_64 and oliphant carries no
// runtime dependency beyond google.golang.org/protobuf (see PLAN.md
// § Fingerprinting). Only the one-shot seeded 64-bit variant is provided;
// that is the only entry point fingerprinting and the public API need.
package xxh3

import (
	"encoding/binary"
	"math/bits"
)

const (
	prime32_1 = 0x9E3779B1
	prime32_2 = 0x85EBCA77
	prime32_3 = 0xC2B2AE3D

	prime64_1 = 0x9E3779B185EBCA87
	prime64_2 = 0xC2B2AE3D27D4EB4F
	prime64_3 = 0x165667B19E3779F9
	prime64_4 = 0x85EBCA77C2B2AE63
	prime64_5 = 0x27D4EB2F165667C5

	primeMX1 = 0x165667919E3779F9
	primeMX2 = 0x9FB21C651E98DF25
)

// xxhash.h: XXH3_kSecret
var kSecret = [192]byte{
	0xb8, 0xfe, 0x6c, 0x39, 0x23, 0xa4, 0x4b, 0xbe, 0x7c, 0x01, 0x81, 0x2c, 0xf7, 0x21, 0xad, 0x1c,
	0xde, 0xd4, 0x6d, 0xe9, 0x83, 0x90, 0x97, 0xdb, 0x72, 0x40, 0xa4, 0xa4, 0xb7, 0xb3, 0x67, 0x1f,
	0xcb, 0x79, 0xe6, 0x4e, 0xcc, 0xc0, 0xe5, 0x78, 0x82, 0x5a, 0xd0, 0x7d, 0xcc, 0xff, 0x72, 0x21,
	0xb8, 0x08, 0x46, 0x74, 0xf7, 0x43, 0x24, 0x8e, 0xe0, 0x35, 0x90, 0xe6, 0x81, 0x3a, 0x26, 0x4c,
	0x3c, 0x28, 0x52, 0xbb, 0x91, 0xc3, 0x00, 0xcb, 0x88, 0xd0, 0x65, 0x8b, 0x1b, 0x53, 0x2e, 0xa3,
	0x71, 0x64, 0x48, 0x97, 0xa2, 0x0d, 0xf9, 0x4e, 0x38, 0x19, 0xef, 0x46, 0xa9, 0xde, 0xac, 0xd8,
	0xa8, 0xfa, 0x76, 0x3f, 0xe3, 0x9c, 0x34, 0x3f, 0xf9, 0xdc, 0xbb, 0xc7, 0xc7, 0x0b, 0x4f, 0x1d,
	0x8a, 0x51, 0xe0, 0x4b, 0xcd, 0xb4, 0x59, 0x31, 0xc8, 0x9f, 0x7e, 0xc9, 0xd9, 0x78, 0x73, 0x64,
	0xea, 0xc5, 0xac, 0x83, 0x34, 0xd3, 0xeb, 0xc3, 0xc5, 0x81, 0xa0, 0xff, 0xfa, 0x13, 0x63, 0xeb,
	0x17, 0x0d, 0xdd, 0x51, 0xb7, 0xf0, 0xda, 0x49, 0xd3, 0x16, 0x55, 0x26, 0x29, 0xd4, 0x68, 0x9e,
	0x2b, 0x16, 0xbe, 0x58, 0x7d, 0x47, 0xa1, 0xfc, 0x8f, 0xf8, 0xb8, 0xd1, 0x7a, 0xd0, 0x31, 0xce,
	0x45, 0xcb, 0x3a, 0x8f, 0x95, 0x16, 0x04, 0x28, 0xaf, 0xd7, 0xfb, 0xca, 0xbb, 0x4b, 0x40, 0x7e,
}

func read32(b []byte, off int) uint64 {
	return uint64(binary.LittleEndian.Uint32(b[off:]))
}

func read64(b []byte, off int) uint64 {
	return binary.LittleEndian.Uint64(b[off:])
}

// xxhash.h: XXH_mult64to128 folded via XXH3_mul128_fold64
func mul128Fold64(lhs, rhs uint64) uint64 {
	hi, lo := bits.Mul64(lhs, rhs)
	return hi ^ lo
}

// xxhash.h: XXH64_avalanche
func avalanche64(h uint64) uint64 {
	h ^= h >> 33
	h *= prime64_2
	h ^= h >> 29
	h *= prime64_3
	h ^= h >> 32
	return h
}

// xxhash.h: XXH3_avalanche
func avalanche(h uint64) uint64 {
	h ^= h >> 37
	h *= primeMX1
	h ^= h >> 32
	return h
}

// xxhash.h: XXH3_rrmxmx
func rrmxmx(h, length uint64) uint64 {
	h ^= bits.RotateLeft64(h, 49) ^ bits.RotateLeft64(h, 24)
	h *= primeMX2
	h ^= (h >> 35) + length
	h *= primeMX2
	return h ^ (h >> 28)
}

// Hash64Seed implements XXH3_64bits_withSeed. A zero seed takes the same
// code paths as the reference's XXH3_64bits.
func Hash64Seed(input []byte, seed uint64) uint64 {
	length := len(input)
	switch {
	case length <= 16:
		return len0to16(input, seed)
	case length <= 128:
		return len17to128(input, seed)
	case length <= 240:
		return len129to240(input, seed)
	default:
		return hashLong(input, seed)
	}
}

// xxhash.h: XXH3_len_0to16_64b (and its 1to3/4to8/9to16 helpers)
func len0to16(input []byte, seed uint64) uint64 {
	length := len(input)
	secret := kSecret[:]
	switch {
	case length > 8: // XXH3_len_9to16_64b
		bitflip1 := (read64(secret, 24) ^ read64(secret, 32)) + seed
		bitflip2 := (read64(secret, 40) ^ read64(secret, 48)) - seed
		inputLo := read64(input, 0) ^ bitflip1
		inputHi := read64(input, length-8) ^ bitflip2
		acc := uint64(length) +
			bits.ReverseBytes64(inputLo) + inputHi +
			mul128Fold64(inputLo, inputHi)
		return avalanche(acc)
	case length >= 4: // XXH3_len_4to8_64b
		seed ^= uint64(bits.ReverseBytes32(uint32(seed))) << 32
		input1 := read32(input, 0)
		input2 := read32(input, length-4)
		bitflip := (read64(secret, 8) ^ read64(secret, 16)) - seed
		input64 := input2 + (input1 << 32)
		return rrmxmx(input64^bitflip, uint64(length))
	case length > 0: // XXH3_len_1to3_64b
		c1 := uint32(input[0])
		c2 := uint32(input[length>>1])
		c3 := uint32(input[length-1])
		combined := (c1 << 16) | (c2 << 24) | c3 | (uint32(length) << 8)
		bitflip := (read32(secret, 0) ^ read32(secret, 4)) + seed
		return avalanche64(uint64(combined) ^ bitflip)
	default:
		return avalanche64(seed ^ read64(secret, 56) ^ read64(secret, 64))
	}
}

// xxhash.h: XXH3_mix16B
func mix16B(input []byte, inOff int, secret []byte, secOff int, seed uint64) uint64 {
	inputLo := read64(input, inOff) ^ (read64(secret, secOff) + seed)
	inputHi := read64(input, inOff+8) ^ (read64(secret, secOff+8) - seed)
	return mul128Fold64(inputLo, inputHi)
}

// xxhash.h: XXH3_len_17to128_64b
func len17to128(input []byte, seed uint64) uint64 {
	length := len(input)
	secret := kSecret[:]
	acc := uint64(length) * prime64_1
	if length > 32 {
		if length > 64 {
			if length > 96 {
				acc += mix16B(input, 48, secret, 96, seed)
				acc += mix16B(input, length-64, secret, 112, seed)
			}
			acc += mix16B(input, 32, secret, 64, seed)
			acc += mix16B(input, length-48, secret, 80, seed)
		}
		acc += mix16B(input, 16, secret, 32, seed)
		acc += mix16B(input, length-32, secret, 48, seed)
	}
	acc += mix16B(input, 0, secret, 0, seed)
	acc += mix16B(input, length-16, secret, 16, seed)
	return avalanche(acc)
}

// xxhash.h: XXH3_len_129to240_64b
func len129to240(input []byte, seed uint64) uint64 {
	const (
		midsizeStartOffset = 3  // XXH3_MIDSIZE_STARTOFFSET
		midsizeLastOffset  = 17 // XXH3_MIDSIZE_LASTOFFSET
		secretSizeMin      = 136
	)
	length := len(input)
	secret := kSecret[:]
	acc := uint64(length) * prime64_1
	nbRounds := length / 16
	for i := 0; i < 8; i++ {
		acc += mix16B(input, 16*i, secret, 16*i, seed)
	}
	acc = avalanche(acc)
	for i := 8; i < nbRounds; i++ {
		acc += mix16B(input, 16*i, secret, 16*(i-8)+midsizeStartOffset, seed)
	}
	acc += mix16B(input, length-16, secret, secretSizeMin-midsizeLastOffset, seed)
	return avalanche(acc)
}

// xxhash.h: XXH3_accumulate_512_scalar
func accumulate512(acc *[8]uint64, input []byte, inOff int, secret []byte, secOff int) {
	for i := 0; i < 8; i++ {
		dataVal := read64(input, inOff+8*i)
		dataKey := dataVal ^ read64(secret, secOff+8*i)
		acc[i^1] += dataVal
		acc[i] += (dataKey & 0xFFFFFFFF) * (dataKey >> 32)
	}
}

// xxhash.h: XXH3_scrambleAcc_scalar
func scrambleAcc(acc *[8]uint64, secret []byte, secOff int) {
	for i := 0; i < 8; i++ {
		acc[i] ^= acc[i] >> 47
		acc[i] ^= read64(secret, secOff+8*i)
		acc[i] *= prime32_1
	}
}

// xxhash.h: XXH3_mix2Accs + XXH3_mergeAccs
func mergeAccs(acc *[8]uint64, secret []byte, secOff int, start uint64) uint64 {
	result := start
	for i := 0; i < 4; i++ {
		result += mul128Fold64(
			acc[2*i]^read64(secret, secOff+16*i),
			acc[2*i+1]^read64(secret, secOff+16*i+8),
		)
	}
	return avalanche(result)
}

// xxhash.h: XXH3_hashLong_64b_withSeed (scalar path). A non-zero seed derives
// a custom secret from kSecret (XXH3_initCustomSecret_scalar).
func hashLong(input []byte, seed uint64) uint64 {
	const (
		secretSize        = 192
		stripeLen         = 64
		secretConsumeRate = 8
		lastAccStart      = 7  // XXH_SECRET_LASTACC_START
		mergeAccsStart    = 11 // XXH_SECRET_MERGEACCS_START
	)
	secret := kSecret[:]
	if seed != 0 {
		custom := make([]byte, secretSize)
		for i := 0; i < secretSize/16; i++ {
			binary.LittleEndian.PutUint64(custom[16*i:], read64(kSecret[:], 16*i)+seed)
			binary.LittleEndian.PutUint64(custom[16*i+8:], read64(kSecret[:], 16*i+8)-seed)
		}
		secret = custom
	}

	length := len(input)
	acc := [8]uint64{prime32_3, prime64_1, prime64_2, prime64_3, prime64_4, prime32_2, prime64_5, prime32_1}

	nbStripesPerBlock := (secretSize - stripeLen) / secretConsumeRate
	blockLen := stripeLen * nbStripesPerBlock
	nbBlocks := (length - 1) / blockLen

	for n := 0; n < nbBlocks; n++ {
		for i := 0; i < nbStripesPerBlock; i++ {
			accumulate512(&acc, input, n*blockLen+i*stripeLen, secret, i*secretConsumeRate)
		}
		scrambleAcc(&acc, secret, secretSize-stripeLen)
	}

	nbStripes := ((length - 1) - blockLen*nbBlocks) / stripeLen
	for i := 0; i < nbStripes; i++ {
		accumulate512(&acc, input, nbBlocks*blockLen+i*stripeLen, secret, i*secretConsumeRate)
	}
	accumulate512(&acc, input, length-stripeLen, secret, secretSize-stripeLen-lastAccStart)

	return mergeAccs(&acc, secret, mergeAccsStart, uint64(length)*prime64_1)
}
