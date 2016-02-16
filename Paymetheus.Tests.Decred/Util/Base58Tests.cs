// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System.Text;
using Xunit;

namespace Paymetheus.Tests.Decred.Util
{
    public static class Base58Tests
    {
        [Theory]
        [InlineData("", "")]
        [InlineData(" ", "Z")]
        [InlineData("-", "n")]
        [InlineData("0", "q")]
        [InlineData("1", "r")]
        [InlineData("-1", "4SU")]
        [InlineData("11", "4k8")]
        [InlineData("abc", "ZiCa")]
        [InlineData("1234598760", "3mJr7AoUXx2Wqd")]
        [InlineData("abcdefghijklmnopqrstuvwxyz", "3yxU3u1igY8WkgtjK92fbJQCd4BZiiT1v25f")]
        [InlineData("00000000000000000000000000000000000000000000000000000000000000", "3sN2THZeE9Eh9eYrwkvZqNstbHGvrxSAM7gXUXvyFQP8XvQLUqNCS27icwUeDT7ckHm4FUHM2mTVh1vbLmk7y")]
        public static void PositiveTests(string decoded, string encoded)
        {
            var decodedBytes = Encoding.UTF8.GetBytes(decoded);

            var encodeResult = Base58.Encode(decodedBytes);
            Assert.Equal(encoded, encodeResult);

            var decodeResult = Base58.Decode(encoded);
            Assert.Equal(decodedBytes, decodeResult);
        }

        [Theory]
        [InlineData("61", "2g")]
        [InlineData("626262", "a3gV")]
        [InlineData("636363", "aPEr")]
        [InlineData("73696d706c792061206c6f6e6720737472696e67", "2cFupjhnEsSn59qHXstmK2ffpLv2")]
        [InlineData("00eb15231dfceb60925886b67d065299925915aeb172c06647", "1NS17iag9jJgTHD1VXjvLCEnZuQ3rJDE9L")]
        [InlineData("516b6fcd0f", "ABnLTmg")]
        [InlineData("bf4f89001e670274dd", "3SEo3LWLoPntC")]
        [InlineData("572e4794", "3EFU7m")]
        [InlineData("ecac89cad93923c02321", "EJDM8drfXA6uyA")]
        [InlineData("10c8511e", "Rt5zm")]
        [InlineData("00000000000000000000", "1111111111")]
        public static void PositiveHexTests(string decodedAsHexString, string encoded)
        {
            var decoded = Hexadecimal.Decode(decodedAsHexString);

            var encodeResult = Base58.Encode(decoded);
            Assert.Equal(encoded, encodeResult);

            var decodeResult = Base58.Decode(encoded);
            Assert.Equal(decoded, decodeResult);
        }

        [Theory]
        [InlineData("0")]
        [InlineData("O")]
        [InlineData("I")]
        [InlineData("l")]
        [InlineData("3mJr0")]
        [InlineData("O3yxU")]
        [InlineData("3sNI")]
        [InlineData("4kl8")]
        [InlineData("0OIl")]
        [InlineData("!@#$%^&*()-_=+~`")]
        public static void InvalidBase58(string invalidBase58)
        {
            byte[] result;
            Assert.False(Base58.TryDecode(invalidBase58, out result));
        }

        [Fact]
        public static void EncodeDoesNotModifyArray()
        {
            var b1 = new byte[] { 0, 1, 2, 3, 4, 5 };
            var b2 = new byte[] { 0, 1, 2, 3, 4, 5 };
            Base58.Encode(b1);
            Assert.Equal(b1, b2);
        }
    }
}
