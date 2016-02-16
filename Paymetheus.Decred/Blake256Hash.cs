// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;

namespace Paymetheus.Decred
{
    struct Blake256HashBuffer
    {
        public const int Length = 32;

        unsafe fixed byte _data[Length];

        public static Blake256HashBuffer CopyFrom(byte[] hash)
        {
            if (hash == null)
                throw new ArgumentNullException(nameof(hash));

            if (hash.Length != Length)
                throw new ArgumentException($"BLAKE256 hash must have byte length {Length}");

            var buffer = default(Blake256HashBuffer);

            unsafe
            {
                byte* p1 = buffer._data;
                fixed (byte* p2 = &hash[0])
                {
                    *(ulong*)(p1) = *(ulong*)(p2);
                    *(ulong*)(p1 + 8) = *(ulong*)(p2 + 8);
                    *(ulong*)(p1 + 16) = *(ulong*)(p2 + 16);
                    *(ulong*)(p1 + 24) = *(ulong*)(p2 + 24);
                }
            }

            return buffer;
        }

        public static bool Equal(ref Blake256HashBuffer first, ref Blake256HashBuffer second)
        {
            unsafe
            {
                fixed (byte* p1 = first._data, p2 = second._data)
                {
                    return *(ulong*)(p1) == *(ulong*)(p2) &&
                           *(ulong*)(p1 + 8) == *(ulong*)(p2 + 8) &&
                           *(ulong*)(p1 + 16) == *(ulong*)(p2 + 16) &&
                           *(ulong*)(p1 + 24) == *(ulong*)(p2 + 24);
                }
            }
        }

        public static string ToString(ref Blake256HashBuffer buffer)
        {
            var reverseCopy = new byte[Length];

            unsafe
            {
                fixed (byte* p = buffer._data)
                {
                    for (int i = 0, j = Length - 1; i < Length; ++i, --j)
                    {
                        reverseCopy[i] = p[j];
                    }
                }
            }

            return Hexadecimal.Encode(reverseCopy);
        }

        public override int GetHashCode()
        {
            unchecked
            {
                unsafe
                {
                    fixed (byte* p = _data)
                    {
                        int hash = 17;
                        for (var i = 0; i < Length; i++)
                            hash = hash * 31 + p[i];
                        return hash;
                    }
                }
            }
        }

        public void WriteToCursor(ByteCursor cursor)
        {
            unsafe
            {
                fixed (byte* p = _data)
                {
                    cursor.WriteFixedBytes(p, Length);
                }
            }
        }
    }

    public sealed class Blake256Hash : IEquatable<Blake256Hash>, ByteCursor.IWriter
    {
        public const int Length = Blake256HashBuffer.Length;

        public static readonly Blake256Hash Zero = new Blake256Hash();

        private Blake256Hash()
        {
            _buffer = default(Blake256HashBuffer);
        }

        public Blake256Hash(byte[] hash)
        {
            _buffer = Blake256HashBuffer.CopyFrom(hash);
        }

        private Blake256HashBuffer _buffer;

        public override string ToString() => Blake256HashBuffer.ToString(ref _buffer);

        public bool Equals(Blake256Hash other)
        {
            if (other == null)
                return false;

            return Blake256HashBuffer.Equal(ref _buffer, ref other._buffer);
        }

        public override bool Equals(object obj) => obj is Blake256Hash && Equals((Blake256Hash)obj);

        public override int GetHashCode() => _buffer.GetHashCode();

        void ByteCursor.IWriter.WriteToCursor(ByteCursor cursor)
        {
            if (cursor == null)
                throw new ArgumentNullException(nameof(cursor));

            _buffer.WriteToCursor(cursor);
        }
    }
}
