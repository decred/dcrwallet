// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Numerics;
using System.Text;

namespace Paymetheus.Decred.Util
{
    /// <summary>
    /// Implements the modified base58 encoding and decoding methods used by Bitcoin wallets.
    /// </summary>
    public static class Base58
    {
        private const int Radix = 58;
        private static readonly BigInteger BigRadix = new BigInteger(Radix);

        private const string Alphabit = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
        private const char AlphabitFirstChar = '1';

        // Maps between ASCII values of base58 digits with the value of the digit.
        // If the index is not the ASCII value of one of the alphabit characters,
        // the value is greater than the radix.
        private static readonly byte[] AsciiToDigitMap = new byte[128]
        {
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 255, 255, 255, 255, 255, 255, 255,
            255, 0, 1, 2, 3, 4, 5, 6,
            7, 8, 255, 255, 255, 255, 255, 255,
            255, 9, 10, 11, 12, 13, 14, 15,
            16, 255, 17, 18, 19, 20, 21, 255,
            22, 23, 24, 25, 26, 27, 28, 29,
            30, 31, 32, 255, 255, 255, 255, 255,
            255, 33, 34, 35, 36, 37, 38, 39,
            40, 41, 42, 43, 255, 44, 45, 46,
            47, 48, 49, 50, 51, 52, 53, 54,
            55, 56, 57, 255, 255, 255, 255, 255,
        };

        public static byte[] Decode(string data)
        {
            byte[] result;
            if (!TryDecode(data, out result))
                throw new Base58Exception("Encoded data is invalid modified base58");
            return result;
        }

        public static bool TryDecode(string encodedData, out byte[] decodedResult)
        {
            if (encodedData == null)
                throw new ArgumentNullException(nameof(encodedData));

            decodedResult = null;

            var answer = BigInteger.Zero;
            var j = BigInteger.One;

            for (var i = encodedData.Length - 1; i >= 0; i--)
            {
                var mapIndex = encodedData[i];
                if (mapIndex >= AsciiToDigitMap.Length)
                    return false;
                var base58Digit = AsciiToDigitMap[mapIndex];
                if (base58Digit >= BigRadix)
                    return false;

                answer += j * base58Digit;
                j *= BigRadix;
            }

            byte[] unpaddedAnswer;
            if (answer == 0)
            {
                // ToByteArray returns byte[1] { 0 } if the value is zero, but
                // this method should return a zero length array.
                unpaddedAnswer = new byte[0];
            }
            else
            {
                unpaddedAnswer = answer.ToByteArray(); // Returns answer little endian.
                Array.Reverse(unpaddedAnswer); // Convert to big endian form.
            }

            var totalLeadingZeros = 0;
            while (totalLeadingZeros < encodedData.Length)
            {
                if (encodedData[totalLeadingZeros] != AlphabitFirstChar)
                    break;

                totalLeadingZeros++;
            }

            var leadingZeros = 0;
            while (leadingZeros < unpaddedAnswer.Length)
            {
                if (unpaddedAnswer[leadingZeros] != 0)
                    break;

                leadingZeros++;
            }

            var extraLeadingZeros = totalLeadingZeros - leadingZeros;
            if (extraLeadingZeros == 0)
            {
                decodedResult = unpaddedAnswer;
                return true;
            }

            var paddedAnswer = new byte[extraLeadingZeros + unpaddedAnswer.Length];
            var sourceIndex = (extraLeadingZeros < 0) ? -extraLeadingZeros : 0;
            var resultIndex = (extraLeadingZeros > 0) ? extraLeadingZeros : 0;
            Array.Copy(unpaddedAnswer, sourceIndex, paddedAnswer, resultIndex, unpaddedAnswer.Length - sourceIndex);

            decodedResult = paddedAnswer;
            return true;
        }

        /// <summary>
        /// Checks that the string is base58 encoded, and that it should decode
        /// without errors, by checking that each character in the string is a base58 digit.
        /// </summary>
        /// <param name="value">The string to check.</param>
        /// <returns>True iff the value is valid base58.</returns>
        public static bool IsBase58Encoded(string value)
        {
            foreach (var ch in value)
            {
                if (!(ch < AsciiToDigitMap.Length && AsciiToDigitMap[ch] < Radix))
                    return false;
            }
            return true;
        }

        public static string Encode(byte[] value)
        {
            // BigInteger constructor interprets the byte array as two's compliment, little endian.
            // Copy the value to avoid modifying the caller's copy, reverse to convert from big endian
            // input to little endian, and add an extra 0 in the MSB position to avoid misinterperting
            // the value as negative.
            var valueCopy = new byte[value.Length + 1];
            for (int i = 0, j = value.Length - 1; i < value.Length; i++, j--)
                valueCopy[i] = value[j];
            var x = new BigInteger(valueCopy);

            var reversedChars = new List<char>();
            while (x > BigInteger.Zero)
            {
                BigInteger remainder;
                x = BigInteger.DivRem(x, BigRadix, out remainder);
                var ch = Alphabit[(int)remainder];
                reversedChars.Add(ch);
            }

            foreach (var b in value)
            {
                if (b != 0)
                    break;

                reversedChars.Add(AlphabitFirstChar);
            }

            var sb = new StringBuilder(reversedChars.Count);
            for (int i = reversedChars.Count - 1; i >= 0; i--)
                sb.Append(reversedChars[i]);
            return sb.ToString();
        }
    }

    public class Base58Exception : Exception
    {
        public Base58Exception(string message) : base(message) { }
    }
}
