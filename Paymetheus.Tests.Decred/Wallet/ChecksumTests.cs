// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using Xunit;

namespace Paymetheus.Tests.Decred.Wallet
{
    public static class ChecksumTests
    {
        [Theory]
        [InlineData("1MirQ9bwyQcGVJPwKUgapu5ouK2E2Ey4gX")]
        [InlineData("12MzCDwodF9G1e7jfwLXfR164RNtx4BRVG")]
        [InlineData("mrX9vMRYLfVy1BnZbc5gZjuyaqH3ZW2ZHz")]
        public static void ValidChecksummedBase58(string value)
        {
            var valueBytes = Base58.Decode(value);
            Assert.True(Checksum.Verify(valueBytes));

            // Clear checksum before WriteSum
            for (var i = valueBytes.Length - Checksum.SumLength; i < valueBytes.Length; i++)
                valueBytes[i] = 0;

            Checksum.WriteSum(valueBytes);
            Assert.True(Checksum.Verify(valueBytes));
        }

        [Theory]
        [InlineData("")]
        [InlineData("1")]
        [InlineData("11")]
        [InlineData("111")]
        [InlineData("1111")]
        [InlineData("3MNQE1Y")]
        public static void InvalidChecksummedBase58(string value)
        {
            var valueBytes = Base58.Decode(value);
            Assert.False(Checksum.Verify(valueBytes));
        }

        [Fact]
        public static void ThrowsForShortBuffer()
        {
            for (var i = 0; i <= Checksum.SumLength; i++)
            {
                var buffer = new byte[i];
                Assert.Throws<ChecksumException>(() => Checksum.WriteSum(buffer));
            }
        }
    }
}
