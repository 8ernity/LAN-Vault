package com.example.lanvault.transfer

import com.example.lanvault.protocol.ManifestEntry
import java.io.File
import java.io.IOException
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.FileAlreadyExistsException
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.text.Normalizer

class PathValidationException(message: String) : IOException(message)

/**
 * Android-side destination planning. The wire path is validated exactly like
 * the desktop receiver (absolute paths, traversal segments, drive letters,
 * control characters, NUL), then every segment is sanitized for the Windows
 * profile before any file or directory is created.
 */
object PathPlanner {
    const val MAX_PATH_BYTES = 4096
    const val MAX_SEGMENT_BYTES = 255

    private val driveRegex = Regex("^[A-Za-z]:$")
    private const val BAD_CHARS = "<>:\"|?*"
    private val reservedNames = setOf(
        "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$",
        "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
        "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"
    )

    fun validateWirePath(path: String): String? {
        if (path.isEmpty()) return "empty"
        if (path.startsWith("/")) return "absolute"
        if (path.contains('\\')) return "backslash"
        for (ch in path) {
            if (ch.code < 0x20 || ch.code == 0x7F) return "control"
        }
        val segments = path.split('/')
        if (segments.any { it.isEmpty() }) return "empty_segment"
        if (segments.any { it == "." || it == ".." }) return "dot_segment"
        if (driveRegex.matches(segments.first())) return "drive_letter"
        if (path.toByteArray(Charsets.UTF_8).size > MAX_PATH_BYTES) return "path_too_long"
        for (segment in segments) {
            if (segment.toByteArray(Charsets.UTF_8).size > MAX_SEGMENT_BYTES) return "segment_too_long"
        }
        return null
    }

    private fun isBidi(ch: Char): Boolean {
        val c = ch.code
        return (c in 0x202A..0x202E) || (c in 0x2066..0x2069)
    }

    fun sanitizeSegment(segment: String): String {
        val normalized = Normalizer.normalize(segment, Normalizer.Form.NFC)
        val sb = StringBuilder(normalized.length)
        for (ch in normalized) {
            if (ch in BAD_CHARS || ch.code < 0x20 || isBidi(ch)) {
                sb.append('_')
            } else {
                sb.append(ch)
            }
        }
        var result = sb.toString().trimEnd(' ', '.')
        if (result.isEmpty()) result = "_"
        val stem = result.substringBefore('.').trimEnd(' ').uppercase()
        if (stem in reservedNames) result = "_$result"
        return result
    }

    private fun osKey(value: String): String = value.lowercase()

    private fun joinPath(parent: String, name: String): String =
        if (parent.isEmpty()) name else "$parent/$name"

    private fun numberedName(name: String, n: Int): String {
        val dot = name.lastIndexOf('.')
        return if (dot <= 0) {
            "$name ($n)"
        } else {
            "${name.substring(0, dot)} ($n)${name.substring(dot)}"
        }
    }

    /**
     * Plans every destination path before any filesystem mutation. Collisions
     * are resolved with numbered suffixes and are independent of manifest order
     * for the directory namespace (all explicit and implied directories are
     * reserved first).
     */
    fun plan(entries: List<ManifestEntry>): List<String> {
        for (entry in entries) {
            if (entry.type != "file" && entry.type != "dir") {
                throw PathValidationException("invalid_entry_type")
            }
            validateWirePath(entry.path)?.let { throw PathValidationException(it) }
            if (entry.type == "file" && entry.size < 0) {
                throw PathValidationException("negative_file_size")
            }
            if (entry.type == "dir" && entry.size != 0L) {
                throw PathValidationException("directory_has_size")
            }
        }

        val seen = HashSet<String>()
        for (entry in entries) {
            if (!seen.add("${entry.type}\t${entry.path}")) {
                throw PathValidationException("duplicate_path")
            }
        }

        val rawFiles = HashSet<String>()
        val rawDirs = HashSet<String>()
        val rawParents = HashSet<String>()
        for (entry in entries) {
            if (entry.type == "file") rawFiles.add(entry.path) else rawDirs.add(entry.path)
            val parts = entry.path.split('/')
            for (i in 1 until parts.size) {
                rawParents.add(parts.subList(0, i).joinToString("/"))
            }
        }
        if (rawFiles.any { it in rawDirs || it in rawParents }) {
            throw PathValidationException("file_dir_conflict")
        }

        data class Segments(val entry: ManifestEntry, val segs: List<String>)
        val plans = entries.map { entry ->
            val segs = entry.path.split('/').map { sanitizeSegment(it) }
            for (segment in segs) {
                if (segment.toByteArray(Charsets.UTF_8).size > MAX_SEGMENT_BYTES) {
                    throw PathValidationException("segment_too_long")
                }
            }
            Segments(entry, segs)
        }

        val dirNames = HashMap<String, String>()
        fun resolveDir(parent: String, segment: String): String {
            val key = osKey(parent) + "\u0000" + osKey(segment)
            return dirNames.getOrPut(key) { segment }
        }

        fun resolveDirectoryPath(segs: List<String>, includeLast: Boolean): String {
            var parent = ""
            val limit = if (includeLast) segs.size else segs.size - 1
            for (i in 0 until limit) {
                parent = joinPath(parent, resolveDir(parent, segs[i]))
            }
            return parent
        }

        val reservedDirs = HashSet<String>()
        for (plan in plans) {
            val full = resolveDirectoryPath(plan.segs, plan.entry.type == "dir")
            if (full.isNotEmpty()) reservedDirs.add(osKey(full))
        }

        // Reject sanitized/case-folded file-vs-directory conflicts.
        for (plan in plans) {
            if (plan.entry.type != "file") continue
            val parent = resolveDirectoryPath(plan.segs, false)
            val candidate = joinPath(parent, plan.segs.last())
            if (osKey(candidate) in reservedDirs) {
                throw PathValidationException("file_dir_conflict")
            }
        }

        val usedFiles = HashSet<String>()
        val result = arrayOfNulls<String>(plans.size)
        for ((index, plan) in plans.withIndex()) {
            if (plan.entry.type != "file") continue
            val parent = resolveDirectoryPath(plan.segs, false)
            var candidate = joinPath(parent, plan.segs.last())
            if (osKey(candidate) in usedFiles) {
                var n = 1
                while (true) {
                    val numbered = joinPath(parent, numberedName(plan.segs.last(), n))
                    if (osKey(numbered) !in usedFiles) {
                        candidate = numbered
                        break
                    }
                    n++
                }
            }
            usedFiles.add(osKey(candidate))
            result[index] = candidate
        }

        return plans.mapIndexed { index, plan ->
            if (plan.entry.type == "dir") {
                resolveDirectoryPath(plan.segs, true)
            } else {
                result[index] ?: throw PathValidationException("unplanned_file")
            }
        }
    }

    /**
     * Returns a safe temp file name for chunked transfers.
     * Checks for collisions with both the final file and any existing .lvpart variants.
     * PC-05-A: Prevents temp file collision between concurrent transfers.
     * 
     * @param finalFile The target final file location
     * @param transferId Transfer identifier (first 8 chars used in name)
     * @return A File object for the temp file that doesn't currently exist
     */
    fun getTempFileName(finalFile: File, transferId: String): File {
        val parent = finalFile.parentFile ?: throw IOException("finalFile has no parent directory")
        val baseName = finalFile.name
        val transferPrefix = transferId.take(8)
        
        // Check primary temp file name
        val primaryTemp = File(parent, "$baseName.$transferPrefix.lvpart")
        if (!primaryTemp.exists()) {
            return primaryTemp
        }
        
        // If collision, append counter
        for (i in 1..9999) {
            val candidate = File(parent, "$baseName.$transferPrefix.$i.lvpart")
            if (!candidate.exists()) {
                return candidate
            }
        }
        
        throw IOException("no available temp file name for $baseName")
    }

    /** Resolves a planned path beneath [root] without following symlinks. */
    fun safeJoin(root: File, relativePath: String): File {
        val rootCanonical = root.canonicalFile
        var current = rootCanonical
        for (segment in relativePath.split('/')) {
            if (segment.isEmpty() || segment == "." || segment == "..") {
                throw PathValidationException("invalid_segment")
            }
            current = File(current, segment)
            if (Files.isSymbolicLink(current.toPath())) {
                throw PathValidationException("symlink_component")
            }
        }
        val candidate = File(rootCanonical, relativePath).canonicalFile
        val rootPath = rootCanonical.toPath().normalize()
        if (!candidate.toPath().normalize().startsWith(rootPath)) {
            throw PathValidationException("path_escapes_destination")
        }
        return candidate
    }

    fun availableFile(target: File): File {
        if (!target.exists()) return target
        val parent = target.parentFile ?: return target
        val name = target.nameWithoutExtension
        val ext = if (target.extension.isNotEmpty()) ".${target.extension}" else ""
        for (i in 1..9999) {
            val candidate = File(parent, "$name ($i)$ext")
            if (!candidate.exists()) return candidate
        }
        throw IOException("no available destination name for ${target.name}")
    }

    /**
     * Atomically moves [part] to a free name at [target]. The destination is
     * never replaced, even if another process created it after planning.
     */
    fun commitNoReplace(part: File, target: File): File {
        var attempt = 0
        var candidate = availableFile(target)
        while (true) {
            try {
                try {
                    Files.move(part.toPath(), candidate.toPath(), StandardCopyOption.ATOMIC_MOVE)
                } catch (_: AtomicMoveNotSupportedException) {
                    Files.move(part.toPath(), candidate.toPath())
                }
                return candidate
            } catch (_: FileAlreadyExistsException) {
                attempt++
                if (attempt > 9999) throw IOException("no available destination name for ${target.name}")
                candidate = availableFile(target)
            }
        }
    }
}
