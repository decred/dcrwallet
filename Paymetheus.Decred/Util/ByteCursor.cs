// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Runtime.InteropServices;

namespace Paymetheus.Decred.Util
{
    public sealed class ByteCursor
    {
        public ByteCursor(byte[] array, int initialOffset = 0)
        {
            if (array == null)
                throw new ArgumentNullException(nameof(array));
            if (initialOffset < 0 || initialOffset >= array.Length)
                throw new IndexOutOfRangeException($"Array of length {array.Length} cannot be indexed at initial offset {initialOffset}");

            _array = array;
            _cursor = initialOffset;
        }

        private readonly byte[] _array;
        private int _cursor;

        public byte ReadByte() => _array[_cursor++];

        public ushort ReadUInt16()
        {
            var v = LittleEndian.ToUInt16(_array, _cursor);
            _cursor += 2;
            return v;
        }

        public uint ReadUInt32()
        {
            var v = LittleEndian.ToUInt32(_array, _cursor);
            _cursor += 4;
            return v;
        }

        public ulong ReadUInt64()
        {
            var v = LittleEndian.ToUInt64(_array, _cursor);
            _cursor += 8;
            return v;
        }

        public short ReadInt16()
        {
            var v = LittleEndian.ToInt16(_array, _cursor);
            _cursor += 2;
            return v;
        }

        public int ReadInt32()
        {
            var v = LittleEndian.ToInt32(_array, _cursor);
            _cursor += 4;
            return v;
        }

        public long ReadInt64()
        {
            var v = LittleEndian.ToInt64(_array, _cursor);
            _cursor += 8;
            return v;
        }

        public ulong ReadCompact()
        {
            int compactSize;
            var v = CompactInt.ReadCompact(_array, _cursor, out compactSize);
            _cursor += compactSize;
            return v;
        }

        public byte[] ReadVarBytes(int maxCount)
        {
            int compactSize;
            var count = CompactInt.ReadCompact(_array, _cursor, out compactSize);
            if (count > (ulong)maxCount)
                throw new Exception($"Deserialized count {count} exceeds maximum allowed value {maxCount}");
            var ret = new byte[count];
            Array.Copy(_array, _cursor + compactSize, ret, 0, (int)count);
            _cursor += compactSize + (int)count;
            return ret;
        }

        public byte[] ReadBytes(int count)
        {
            var ret = new byte[count];
            Array.Copy(_array, _cursor, ret, 0, count);
            _cursor += count;
            return ret;
        }

        public void WriteByte(byte value) => _array[_cursor++] = value;

        public void WriteUInt16(ushort value)
        {
            LittleEndian.WriteUInt16(_array, _cursor, value);
            _cursor += 2;
        }

        public void WriteUInt32(uint value)
        {
            LittleEndian.WriteUInt32(_array, _cursor, value);
            _cursor += 4;
        }

        public void WriteUInt64(ulong value)
        {
            LittleEndian.WriteUInt64(_array, _cursor, value);
            _cursor += 8;
        }

        public void WriteInt16(short value)
        {
            unchecked { WriteUInt16((ushort)value); }
        }

        public void WriteInt32(int value)
        {
            unchecked { WriteUInt32((uint)value); }
        }

        public void WriteInt64(long value)
        {
            unchecked { WriteUInt64((ulong)value); }
        }

        public void WriteCompact(ulong value)
        {
            var compactSize = CompactInt.WriteCompact(_array, _cursor, value);
            _cursor += compactSize;
        }

        public void WriteVarBytes(byte[] value)
        {
            var compactSize = CompactInt.WriteCompact(_array, _cursor, (ulong)value.Length);
            Array.Copy(value, 0, _array, _cursor + compactSize, value.Length);
            _cursor += compactSize + value.Length;
        }

        public void WriteBytes(byte[] value)
        {
            Array.Copy(value, 0, _array, _cursor, value.Length);
            _cursor += value.Length;
        }

        public unsafe void WriteFixedBytes(byte* value, int valueLength)
        {
            if (valueLength < 0 || _cursor > _array.Length - valueLength)
                throw new IndexOutOfRangeException();
            Marshal.Copy((IntPtr)value, _array, _cursor, valueLength);
            _cursor += valueLength;
        }

        public interface IWriter
        {
            void WriteToCursor(ByteCursor cursor);
        }

        public void Write<W>(W writer) where W : IWriter
        {
            writer.WriteToCursor(this);
        }
    }
}
