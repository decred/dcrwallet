// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Decred.Wallet
{
    public static class PgpWordListTests
    {
        private static PgpWordList PgpWordList = new PgpWordList();

        public static IEnumerable<object[]> PositiveTests()
        {
            return new[]
            {
                new object[] {
                    "E58294F2E9A227486E8B061B31CC528FD7FA3F19",
                    "topmost Istanbul Pluto vagabond treadmill Pacific brackish dictator goldfish Medusa afflict bravado chatter revolver Dupont midsummer stopwatch whimsical cowbell bottomless",
                },
                new object[] {
                    "D1D464C004F00FB5C9A4C8D8E433E7FB7FF56256",
                    "stairway souvenir flytrap recipe adrift upcoming artist positive spearhead Pandora spaniel stupendous tonic concurrent transit Wichita lockup visitor flagpole escapade",
                },
            };
        }

        [Theory]
        [MemberData(nameof(PositiveTests))]
        public static void ValidEncodings(string hexEncoding, string pgpWordListEncoding)
        {
            var bytes = Hexadecimal.Decode(hexEncoding);
            var expectedWords = pgpWordListEncoding.Split();

            var encodedBytes = PgpWordList.Encode(bytes);
            Assert.Equal(expectedWords, encodedBytes);
        }

        [Theory]
        [MemberData(nameof(PositiveTests))]
        public static void ValidDecodings(string hexEncoding, string pgpWordListEncoding)
        {
            var words = pgpWordListEncoding.Split();
            var expectedBytes = Hexadecimal.Decode(hexEncoding);

            var decodedBytes = PgpWordList.Decode(words);
            Assert.Equal(expectedBytes, decodedBytes);
        }

        [Theory]
        [MemberData(nameof(PositiveTests))]
        public static void DecodeIsCaseInsensitive(string hexEncoding, string pgpWordListEncoding)
        {
            var expectedBytes = Hexadecimal.Decode(hexEncoding);

            var upperCaseWords = pgpWordListEncoding.ToUpper().Split();
            var decodedUpperCase = PgpWordList.Decode(upperCaseWords);
            Assert.Equal(expectedBytes, decodedUpperCase);

            var lowerCaseWords = pgpWordListEncoding.ToLower().Split();
            var decodedLowerCase = PgpWordList.Decode(lowerCaseWords);
            Assert.Equal(expectedBytes, decodedLowerCase);
        }

        [Fact]
        public static void DecodeThrowsForInvalidWord()
        {
            // Any word not described by the word list is sufficient, but a string
            // with only whitespace is guaranteed to not be a part of the list.
            var invalidWord = new string[] { " " };
            Assert.Throws<PgpWordListInvalidEncodingException>(() => PgpWordList.Decode(invalidWord));
        }

        private static string EvenWord(byte b) => PgpWordList.Encode(new byte[] { b })[0];

        private static string OddWord(byte b) => PgpWordList.Encode(new byte[] { 0, b })[1];

        public static IEnumerable<object[]> ValidWordsAtInvalidIndexes()
        {
            return new[]
            {
                new object[] { new string[] { OddWord(0) } },
                new object[] { new string[] { EvenWord(50), EvenWord(50) } },
                new object[] { new string[] { EvenWord(100), OddWord(101), OddWord(102) } },
                new object[] { new string[] { EvenWord(254), EvenWord(255), EvenWord(255) } },
            };
        }

        [Theory]
        [MemberData(nameof(ValidWordsAtInvalidIndexes))]
        public static void DecodeThrowsForInvalidIndexes(string[] pgpWordListEncoding)
        {
            Assert.Throws<PgpWordListInvalidEncodingException>(() => PgpWordList.Decode(pgpWordListEncoding));
        }

        [Theory]
        [InlineData("topmost Istanbul Pluto vagabond treadmill brackish dictator goldfish Medusa afflict bravado chatter revolver Dupont midsummer stopwatch whimsical cowbell bottomless")]
        [InlineData("souvenir flytrap recipe adrift upcoming artist positive spearhead Pandora spaniel stupendous tonic concurrent transit Wichita lockup visitor flagpole escapade")]
        public static void DecodeThrowsForMissingWords(string pgpWordListEncoding)
        {
            var words = pgpWordListEncoding.Split();
            Assert.Throws<PgpWordListInvalidEncodingException>(() => PgpWordList.Decode(words));
        }
    }
}
