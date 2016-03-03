// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred.Wallet
{
    public static class Checksum
    {
        public const int SumLength = 4;

        /// <summary>
        /// Verifies the value ends with 4 bytes of a matching checksum.
        /// </summary>
        /// <param name="value">Byte array containing value to check, followed by 4 bytes of checksum</param>
        /// <returns>True iff the value is long enough to contain a checksum and the checksum matches.</returns>
        public static bool Verify(byte[] value)
        {
            if (value == null)
                throw new ArgumentNullException(nameof(value));

            if (value.Length <= SumLength)
                return false;

            var hash = Hash(value);

            unsafe
            {
                fixed (byte* valueHash = &value[value.Length - SumLength], expectedHash = &hash[0])
                {
                    return *(uint*)valueHash == *(uint*)expectedHash;
                }
            }
        }

        /// <summary>
        /// Computes the checksum for all but the last 4 bytes of the buffer and writes the
        /// sum to the final 4 bytes.
        /// </summary>
        /// <param name="buffer">Buffer containing value to sum, followed by 4 bytes to place checksum</param>
        public static void WriteSum(byte[] buffer)
        {
            if (buffer == null)
                throw new ArgumentNullException(nameof(buffer));

            if (buffer.Length <= SumLength)
                throw new ChecksumException($"Buffer of length {buffer.Length} is too small to write checksum");

            var hash = Hash(buffer);

            unsafe
            {
                fixed (byte* bufferHash = &buffer[buffer.Length - SumLength], computedHash = &hash[0])
                {
                    *(uint*)bufferHash = *(uint*)computedHash;
                }
            }
        }

        // Returned array is guaranteed to be non-null and safe to index into the first 4
        // bytes using pointer arithmetic.
        private static byte[] Hash(byte[] value)
        {
            byte[] hash;
            using (var hasher = new Blake256())
            {
                var intermediateHash = hasher.ComputeHash(value, 0, value.Length - SumLength);
                hash = hasher.ComputeHash(intermediateHash);
            }

            if (hash.Length != Blake256Hash.Length || hash.Length < SumLength)
                throw new Exception($"Double-BLAKE256 result has improper length {hash.Length}");

            return hash;
        }
    }

    public class ChecksumException : Exception
    {
        public ChecksumException(string message) : base(message) { }
    }
}
