package com.example.lanvault.protocol

import org.junit.Assert.*
import org.junit.Test
import java.util.Base64

class BitmapTest {

    @Test
    fun testBitmapMatchesTestVectors() {
        val totalChunks = 10
        val bitmap = ChunkBitmap(totalChunks)

        bitmap.set(0)
        bitmap.set(1)
        bitmap.set(2)
        bitmap.set(9)

        val bytes = bitmap.toByteArray()
        val hex = bytes.joinToString("") { "%02x".format(it) }
        assertEquals("0702", hex)

        val b64 = Base64.getEncoder().encodeToString(bytes)
        assertEquals("BwI=", b64)

        // Test restoring from Base64
        val decoded = ChunkBitmap.fromBase64(10, "BwI=")
        assertTrue(decoded.isSet(0))
        assertTrue(decoded.isSet(1))
        assertTrue(decoded.isSet(2))
        assertTrue(decoded.isSet(9))
        assertFalse(decoded.isSet(3))
        assertFalse(decoded.isSet(8))
        assertFalse(decoded.isComplete())

        // Set remaining and check isComplete
        for (i in 0 until 10) {
            decoded.set(i)
        }
        assertTrue(decoded.isComplete())
    }

    @Test
    fun testInvalidBitmaps() {
        // Wrong length (1 byte for 10 chunks needs 2 bytes)
        try {
            ChunkBitmap.fromBase64(10, "Bw==")
            fail("Expected exception for wrong length")
        } catch (e: IllegalArgumentException) {
            // Success
        }

        // Bit set beyond chunk_count (index 10 is set in AAQ= which is 00 04)
        try {
            ChunkBitmap.fromBase64(10, "AAQ=")
            fail("Expected exception for bit set beyond chunk_count")
        } catch (e: IllegalArgumentException) {
            // Success
        }
    }
}
