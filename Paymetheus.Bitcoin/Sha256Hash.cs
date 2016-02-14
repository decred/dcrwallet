// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Util;
using System;

namespace Paymetheus.Bitcoin
{
    struct Sha256HashBuffer
    {
        public const int Length = 32;

        unsafe fixed byte _data[Length];

        public static Sha256HashBuffer CopyFrom(byte[] hash)
        {
            if (hash == null)
                throw new ArgumentNullException(nameof(hash));

            if (hash.Length != Length)
                throw new ArgumentException($"SHA256 hash must have byte length {Length}");

            var buffer = default(Sha256HashBuffer);

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

        public static bool Equal(ref Sha256HashBuffer first, ref Sha256HashBuffer second)
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

        public static string ToString(ref Sha256HashBuffer buffer)
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

    public sealed class Sha256Hash : IEquatable<Sha256Hash>, ByteCursor.IWriter
    {
        public const int Length = Sha256HashBuffer.Length;

        public static readonly Sha256Hash Zero = new Sha256Hash();

        private Sha256Hash()
        {
            _buffer = default(Sha256HashBuffer);
        }

        public Sha256Hash(byte[] hash)
        {
            _buffer = Sha256HashBuffer.CopyFrom(hash);
        }

        private Sha256HashBuffer _buffer;

        public override string ToString() => Sha256HashBuffer.ToString(ref _buffer);

        public bool Equals(Sha256Hash other)
        {
            if (other == null)
                return false;

            return Sha256HashBuffer.Equal(ref _buffer, ref other._buffer);
        }

        public override bool Equals(object obj) => obj is Sha256Hash && Equals((Sha256Hash)obj);

        public override int GetHashCode() => _buffer.GetHashCode();

        void ByteCursor.IWriter.WriteToCursor(ByteCursor cursor)
        {
            _buffer.WriteToCursor(cursor);
        }
    }
}
