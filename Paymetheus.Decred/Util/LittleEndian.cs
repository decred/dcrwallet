// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred.Util
{
    // System.BitConverter uses host endianness, so explicitly implement the byteorder
    // methods for little endian.
    public static class LittleEndian
    {
        private static void BoundsCheck(byte []array, int offset, int fieldLength)
        {
            if (array == null)
                throw new ArgumentNullException(nameof(array));
            if (offset < 0)
                throw new IndexOutOfRangeException("Negative index");
            if (offset > array.Length - fieldLength)
                throw new IndexOutOfRangeException($"High index {offset + fieldLength - 1} cannot be accessed in array of length {array.Length}");
        }

        public static ushort ToUInt16(byte[] array, int offset)
        {
            BoundsCheck(array, offset, 2);

            unsafe
            {
                fixed (byte* p = &array[offset])
                {
                    return (ushort)(*p | (uint)(*(p + 1) << 8));
                }
            }
        }

        public static uint ToUInt32(byte[] array, int offset)
        {
            BoundsCheck(array, offset, 4);

            unsafe
            {
                fixed (byte* p = &array[offset])
                {
                    return *p | ((uint)*(p + 1) << 8) | ((uint)*(p + 2) << 16) | ((uint)*(p + 3) << 24);
                }
            }
        }

        public static ulong ToUInt64(byte[] array, int offset)
        {
            BoundsCheck(array, offset, 8);

            unsafe
            {
                fixed (byte* p = &array[offset])
                {
                    return *p | ((ulong)*(p + 1) << 8) | ((ulong)*(p + 2) << 16) | ((ulong)*(p + 3) << 24) |
                        ((ulong)*(p + 4) << 32) | ((ulong)*(p + 5) << 40) | ((ulong)*(p + 6) << 48) | ((ulong)*(p + 7) << 56);
                }
            }
        }

        public static short ToInt16(byte[] array, int offset)
        {
            unchecked { return (short)ToUInt16(array, offset); }
        }

        public static int ToInt32(byte[] array, int offset)
        {
            unchecked { return (int)ToUInt32(array, offset); }
        }

        public static long ToInt64(byte[] array, int offset)
        {
            unchecked { return (long)ToUInt64(array, offset); }
        }

        public static void WriteUInt16(byte[] array, int offset, ushort value)
        {
            BoundsCheck(array, offset, 2);

            unchecked
            {
                unsafe
                {
                    fixed (byte* p = &array[offset])
                    {
                        *p = (byte)value;
                        *(p + 1) = (byte)(value >> 8);
                    }
                }
            }
        }

        public static void WriteUInt32(byte[] array, int offset, uint value)
        {
            BoundsCheck(array, offset, 4);

            unchecked
            {
                unsafe
                {
                    fixed (byte* p = &array[offset])
                    {
                        *p = (byte)value;
                        *(p + 1) = (byte)(value >> 8);
                        *(p + 2) = (byte)(value >> 16);
                        *(p + 3) = (byte)(value >> 24);
                    }
                }
            }
        }

        public static void WriteUInt64(byte[] array, int offset, ulong value)
        {
            BoundsCheck(array, offset, 8);

            unchecked
            {
                unsafe
                {
                    fixed (byte* p = &array[offset])
                    {
                        *p = (byte)value;
                        *(p + 1) = (byte)(value >> 8);
                        *(p + 2) = (byte)(value >> 16);
                        *(p + 3) = (byte)(value >> 24);
                        *(p + 4) = (byte)(value >> 32);
                        *(p + 5) = (byte)(value >> 40);
                        *(p + 6) = (byte)(value >> 48);
                        *(p + 7) = (byte)(value >> 56);
                    }
                }
            }
        }

        public static void WriteInt16(byte[] array, int offset, short value)
        {
            unchecked { WriteUInt16(array, offset, (ushort)value); }
        }

        public static void WriteInt32(byte[] array, int offset, int value)
        {
            unchecked { WriteUInt32(array, offset, (uint)value); }
        }

        public static void WriteInt64(byte[] array, int offset, long value)
        {
            unchecked { WriteUInt64(array, offset, (ulong)value); }
        }
    }
}
