// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred.Util
{
    public static class CompactInt
    {
        public static int SerializeSize(ulong value)
        {
            // Choose the smallest of these possible encodings:
            //   - 1 byte that is not any of 0xfd, 0xfe, or 0xff
            //   - 1 byte discriminant (0xfd) + 2 bytes for 16-bit integer
            //   - 1 byte discriminant (0xfe) + 4 bytes for 32-bit integer
            //   - 1 byte discriminant (0xff) + 8 bytes for 64-bit integer
            if (value < 0xfd) return 1;
            if (value <= ushort.MaxValue) return 3;
            if (value <= uint.MaxValue) return 5;
            return 9;
        }

        /// <summary>
        ///  Writes the compact encoding of value to a destination byte array, starting at offset.
        /// </summary>
        /// <returns>The number of bytes written to destination.</returns>
        public static int WriteCompact(byte[] destination, int offset, ulong value)
        {
            if (destination == null)
                throw new ArgumentNullException(nameof(destination));

            if (value < 0xfd)
            {
                destination[offset] = (byte)value;
                return 1;
            }
            else if (value <= ushort.MaxValue)
            {
                destination[offset] = 0xfd;
                LittleEndian.WriteUInt16(destination, offset + 1, (ushort)value);
                return 3;
            }
            else if (value <= uint.MaxValue)
            {
                destination[offset] = 0xfe;
                LittleEndian.WriteUInt32(destination, offset + 1, (uint)value);
                return 5;
            }
            else
            {
                destination[offset] = 0xff;
                LittleEndian.WriteUInt64(destination, offset + 1, value);
                return 9;
            }
        }

        public static ulong ReadCompact(byte[] source, int offset, out int bytesRead)
        {
            if (source == null)
                throw new ArgumentNullException(nameof(source));

            var discriminant = source[offset];
            switch (discriminant)
            {
                case 0xfd:
                    bytesRead = 3;
                    return CanonicalCheck(LittleEndian.ToUInt16(source, offset + 1), 0xfd, discriminant);
                case 0xfe:
                    bytesRead = 5;
                    return CanonicalCheck(LittleEndian.ToUInt32(source, offset + 1), 0x10000, discriminant);
                case 0xff:
                    bytesRead = 9;
                    return CanonicalCheck(LittleEndian.ToUInt64(source, offset + 1), 0x100000000, discriminant);
                default:
                    bytesRead = 1;
                    return discriminant;
            }
        }

        private static ulong CanonicalCheck(ulong value, ulong minValue, byte discriminant)
        {
            if (value < minValue)
            {
                var message = $"Discriminant {discriminant} must encode a value no smaller than {minValue}";
                throw new CanonicalCompactIntException(message);
            }
            return value;
        }
    }

    public class CanonicalCompactIntException : Exception
    {
        public CanonicalCompactIntException(string message) : base(message) { }
    }
}
