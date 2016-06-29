// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Security.Cryptography;
using System.Text;
using Xunit;
using Paymetheus.Decred.Wallet;

namespace Paymetheus.Tests.Decred
{
    public static class Sha256Tests
    {
        [Fact]
        public static void DoubleSha256Test()
        {
            var innerExpected = new byte[] {
                0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea,
                0x41, 0x41, 0x40, 0xde, 0x5d, 0xae, 0x22, 0x23,
                0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c,
                0xb4, 0x10, 0xff, 0x61, 0xf2, 0x00, 0x15, 0xad,
            };
            var vector = Encoding.ASCII.GetBytes("abc");
            var hasher = new SHA256Managed();
            var innerDigest = hasher.ComputeHash(vector);

            Assert.Equal(innerDigest, innerExpected);

            var outerExpected = new byte[] {
                0x4f, 0x8b, 0x42, 0xc2, 0x2d, 0xd3, 0x72, 0x9b,
                0x51, 0x9b, 0xa6, 0xf6, 0x8d, 0x2d, 0xa7, 0xcc,
                0x5b, 0x2d, 0x60, 0x6d, 0x05, 0xda, 0xed, 0x5a,
                0xd5, 0x12, 0x8c, 0xc0, 0x3e, 0x6c, 0x63, 0x58,
            };
            var outerDigest = hasher.ComputeHash(innerDigest);

            Assert.Equal(outerDigest, outerExpected);

            Assert.Equal(outerDigest, WalletSeed.DoubleSha256(vector));
        }
    }
}
