package com.example.lanvault.protocol

import java.util.Base64

class ChunkBitmap(
    val chunkCount: Int,
    private val bytes: ByteArray
) {
    init {
        val expectedLen = (chunkCount + 7) / 8
        if (bytes.size != expectedLen) {
            throw IllegalArgumentException("Bitmap byte length (${bytes.size}) does not match expected ceil(chunk_count/8) ($expectedLen)")
        }
        // Verify no bits are set beyond chunkCount limit
        for (i in chunkCount until expectedLen * 8) {
            val byteIdx = i / 8
            val bitIdx = i % 8
            if ((bytes[byteIdx].toInt() and (1 shl bitIdx)) != 0) {
                throw IllegalArgumentException("Bitmap has bit set beyond chunk_count limit ($chunkCount)")
            }
        }
    }

    constructor(chunkCount: Int) : this(chunkCount, ByteArray((chunkCount + 7) / 8))

    fun getBytes(): ByteArray = bytes.copyOf()
    fun toByteArray(): ByteArray = bytes.copyOf()

    fun set(chunkIndex: Int) {
        if (chunkIndex < 0 || chunkIndex >= chunkCount) {
            throw IndexOutOfBoundsException("Chunk index $chunkIndex out of bounds [0, $chunkCount)")
        }
        val byteIdx = chunkIndex / 8
        val bitIdx = chunkIndex % 8
        bytes[byteIdx] = (bytes[byteIdx].toInt() or (1 shl bitIdx)).toByte()
    }

    fun clear(chunkIndex: Int) {
        if (chunkIndex < 0 || chunkIndex >= chunkCount) {
            throw IndexOutOfBoundsException("Chunk index $chunkIndex out of bounds [0, $chunkCount)")
        }
        val byteIdx = chunkIndex / 8
        val bitIdx = chunkIndex % 8
        bytes[byteIdx] = (bytes[byteIdx].toInt() and (1 shl bitIdx).inv()).toByte()
    }

    fun isSet(chunkIndex: Int): Boolean {
        if (chunkIndex < 0 || chunkIndex >= chunkCount) return false
        val byteIdx = chunkIndex / 8
        val bitIdx = chunkIndex % 8
        return (bytes[byteIdx].toInt() and (1 shl bitIdx)) != 0
    }

    fun completedCount(): Int {
        var count = 0
        for (i in 0 until chunkCount) {
            if (isSet(i)) count++
        }
        return count
    }

    fun isComplete(): Boolean = completedCount() == chunkCount

    fun toBase64(): String = Base64.getEncoder().encodeToString(bytes)

    fun toHex(): String {
        val sb = StringBuilder(bytes.size * 2)
        for (b in bytes) {
            sb.append(String.format("%02x", b))
        }
        return sb.toString()
    }

    companion object {
        fun fromBytes(chunkCount: Int, data: ByteArray): ChunkBitmap {
            return ChunkBitmap(chunkCount, data)
        }

        fun fromBase64(chunkCount: Int, base64Str: String): ChunkBitmap {
            val decoded = Base64.getDecoder().decode(base64Str)
            return ChunkBitmap(chunkCount, decoded)
        }
    }
}
