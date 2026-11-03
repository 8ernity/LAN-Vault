package com.example.lanvault.transfer

import com.example.lanvault.protocol.ManifestEntry
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.io.File

class PathPlannerTest {

    @Test
    fun testValidateWirePathRejections() {
        assertEquals("empty", PathPlanner.validateWirePath(""))
        assertEquals("absolute", PathPlanner.validateWirePath("/foo/bar"))
        assertEquals("backslash", PathPlanner.validateWirePath("foo\\bar"))
        assertEquals("dot_segment", PathPlanner.validateWirePath(".."))
        assertEquals("dot_segment", PathPlanner.validateWirePath("."))
        assertEquals("dot_segment", PathPlanner.validateWirePath("foo/../bar"))
        assertEquals("dot_segment", PathPlanner.validateWirePath("foo/./bar"))
        assertEquals("empty_segment", PathPlanner.validateWirePath("foo//bar"))
        assertEquals("drive_letter", PathPlanner.validateWirePath("C:/test.txt"))
        assertEquals("control", PathPlanner.validateWirePath("foo\u0000bar"))
        assertEquals("control", PathPlanner.validateWirePath("foo\u001Fbar"))
        assertEquals("control", PathPlanner.validateWirePath("foo\u007Fbar"))

        // Valid paths
        assertNull(PathPlanner.validateWirePath("hello.txt"))
        assertNull(PathPlanner.validateWirePath("dir/sub/file.bin"))
        assertNull(PathPlanner.validateWirePath("données/été.txt"))
        assertNull(PathPlanner.validateWirePath("日本語/ファイル.txt"))
        assertNull(PathPlanner.validateWirePath(".hidden"))
        assertNull(PathPlanner.validateWirePath("a..b"))
    }

    @Test
    fun testSanitizeSegment() {
        // Bad chars
        assertEquals("a_b_c", PathPlanner.sanitizeSegment("a<b:c"))
        assertEquals("a_b", PathPlanner.sanitizeSegment("a*b"))
        assertEquals("a_b", PathPlanner.sanitizeSegment("a?b"))
        assertEquals("a_b", PathPlanner.sanitizeSegment("a\"b"))
        assertEquals("a_b", PathPlanner.sanitizeSegment("a|b"))

        // Trailing dot and space
        assertEquals("foo", PathPlanner.sanitizeSegment("foo."))
        assertEquals("foo", PathPlanner.sanitizeSegment("foo "))
        assertEquals("foo", PathPlanner.sanitizeSegment("foo. ."))

        // Reserved names
        assertEquals("_CON", PathPlanner.sanitizeSegment("CON"))
        assertEquals("_con.txt", PathPlanner.sanitizeSegment("con.txt"))
        assertEquals("_PRN", PathPlanner.sanitizeSegment("PRN"))
        assertEquals("_aux.dat", PathPlanner.sanitizeSegment("aux.dat"))
        assertEquals("_NUL", PathPlanner.sanitizeSegment("NUL"))
        assertEquals("_COM1.txt", PathPlanner.sanitizeSegment("COM1.txt"))
        assertEquals("_LPT9.log", PathPlanner.sanitizeSegment("LPT9.log"))

        // Empty after trim
        assertEquals("_", PathPlanner.sanitizeSegment("   "))
        assertEquals("_", PathPlanner.sanitizeSegment("..."))
    }

    @Test
    fun testPlanManifestOrderIndependentFileDirConflict() {
        // Manifest where file comes before dir
        val entries1 = listOf(
            ManifestEntry(index = 0, path = "foo", type = "file", size = 100, mtimeMs = 1000),
            ManifestEntry(index = 1, path = "foo/bar.txt", type = "file", size = 200, mtimeMs = 1000)
        )
        try {
            PathPlanner.plan(entries1)
            fail("Expected file_dir_conflict exception")
        } catch (e: PathValidationException) {
            assertEquals("file_dir_conflict", e.message)
        }

        // Manifest where dir comes before file
        val entries2 = listOf(
            ManifestEntry(index = 0, path = "foo", type = "dir", size = 0, mtimeMs = 1000),
            ManifestEntry(index = 1, path = "foo", type = "file", size = 100, mtimeMs = 1000)
        )
        try {
            PathPlanner.plan(entries2)
            fail("Expected file_dir_conflict exception")
        } catch (e: PathValidationException) {
            assertEquals("file_dir_conflict", e.message)
        }
    }

    @Test
    fun testPlanCollisionResolution() {
        val entries = listOf(
            ManifestEntry(index = 0, path = "file.txt", type = "file", size = 10, mtimeMs = 1000),
            ManifestEntry(index = 1, path = "FILE.TXT", type = "file", size = 10, mtimeMs = 1000),
            ManifestEntry(index = 2, path = "file:txt", type = "file", size = 10, mtimeMs = 1000)
        )
        val planned = PathPlanner.plan(entries)
        assertEquals(3, planned.size)
        assertEquals("file.txt", planned[0])
        assertEquals("FILE (1).TXT", planned[1])
        assertEquals("file_txt", planned[2])
    }

    @Test
    fun testSafeJoinRejectsTraversal() {
        val tempDir = File(System.getProperty("java.io.tmpdir"), "lanvault_test_" + System.currentTimeMillis())
        tempDir.mkdirs()
        try {
            try {
                PathPlanner.safeJoin(tempDir, "../escaped.txt")
                fail("Expected traversal exception")
            } catch (e: PathValidationException) {
                // Good
            }

            // Normal safe join works
            val safe = PathPlanner.safeJoin(tempDir, "sub/file.txt")
            assertTrue(safe.canonicalPath.startsWith(tempDir.canonicalPath))
        } finally {
            tempDir.deleteRecursively()
        }
    }

    @Test
    fun testCommitNoReplace() {
        val tempDir = File(System.getProperty("java.io.tmpdir"), "lanvault_commit_" + System.currentTimeMillis())
        tempDir.mkdirs()
        try {
            val target = File(tempDir, "report.pdf")
            target.writeText("original content")

            val part = File(tempDir, "report.pdf.part")
            part.writeText("new content")

            val committed = PathPlanner.commitNoReplace(part, target)
            // Target was not overwritten!
            assertEquals("original content", target.readText())
            // Committed was created with collision suffix
            assertEquals(File(tempDir, "report (1).pdf").canonicalPath, committed.canonicalPath)
            assertEquals("new content", committed.readText())
        } finally {
            tempDir.deleteRecursively()
        }
    }
}
