// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Text;

namespace Paymetheus.Decred.Util
{
    public static class Hexadecimal
    {
        public static byte[] Decode(string value)
        {
            byte[] result;
            if (!TryDecode(value, out result))
                throw new HexadecimalEncodingException();
            return result;
        }

        public static bool TryDecode(string value, out byte[] result)
        {
            if (value == null)
                throw new ArgumentNullException(nameof(value));

            if (value.Length % 2 != 0)
            {
                result = null;
                return false;
            }

            var decodedResult = new byte[value.Length / 2];
            for (int i = 0; i < value.Length; i += 2)
            {
                var first = value[i];
                var second = value[i + 1];

                if (!IsHexDigit(first) || !IsHexDigit(second))
                {
                    result = null;
                    return false;
                }

                decodedResult[i / 2] = (byte)((uint)HexDigitToByte(first) << 4 | HexDigitToByte(second));
            }

            result = decodedResult;
            return true;
        }

        private static bool IsHexDigit(char ch) =>
            (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F');
        
        private static byte HexDigitToByte(char ch)
        {
            if (ch >= 'a')
                return (byte)(ch - 'a' + 10);
            else if (ch >= 'A')
                return (byte)(ch - 'A' + 10);
            else
                return (byte)(ch - '0');
        }

        public static string Encode(byte[] bytes)
        {
            if (bytes == null)
                throw new ArgumentNullException(nameof(bytes));

            var s = new StringBuilder(bytes.Length * 2);
            foreach (var b in bytes)
            {
                s.Append($"{b:x2}");
            }
            return s.ToString();
        }
    }

    public class HexadecimalEncodingException : Exception
    {
        public HexadecimalEncodingException() : base("Value is not a valid hexadecimal string") { }
    }
}
