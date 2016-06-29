// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.using Paymetheus.Decred.Wallet;

using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using System;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Decred.Wallet
{
    public static class WalletSeedTests
    {
        private static PgpWordList PgpWordList = new PgpWordList();

        public static IEnumerable<object[]> NegativeTests()
        {
            return new[]
            {
                // invalid word
                new object[] {
                    "deckhand hydraulic preshrunk amusement beeswax suspicious moo customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman maritime spearhead",
                    typeof(PgpWordListInvalidEncodingException),
                },

                // invalid hex
                new object[] {
                    "z97497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688",
                    typeof(HexadecimalEncodingException),
                },

                // not enough hex
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd6",
                    typeof(Exception),
                },

                // too much hex
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd6888888",
                    typeof(Exception),
                },

                // not enough words
                new object[] {
                    "deckhand hydraulic preshrunk amusement beeswax suspicious talon customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman",
                    typeof(Exception),
                },

                // too many words
                new object[] {
                    "deckhand hydraulic preshrunk amusement beeswax suspicious talon customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman maritime spearhead hydraulic",
                    typeof(Exception),
                },

                // with invalid checksum
                new object[] {
                    "deckhand hydraulic preshrunk amusement beeswax suspicious talon customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman maritime stockman",
                    typeof(ChecksumException),
                },

                // hex with invalid checksum
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688c7",
                    typeof(ChecksumException),
                },
            };
        }

        public static IEnumerable<object[]> PositiveTests()
        {
            return new[]
            {
                // with checksum
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688c9",
                    "deckhand hydraulic preshrunk amusement beeswax suspicious talon customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman maritime spearhead",
                },

                // without checksum
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688",
                    "deckhand hydraulic preshrunk amusement beeswax suspicious talon customer spigot therapist swelter Saturday miser microscope stairway maverick ribcage designing playhouse unify rebirth guitarist bombast consensus dwelling Waterloo printer mosquito select document stockman maritime",
                },

                // identical hex 32 bytes
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688",
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688",
                },

                // hex with checksum
                new object[] {
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688c9",
                    "497497071bdbdf3fccdfddcf828dd18aac4493eda269253753f99897b84fd688c9",
                },
            };
        }

        [Theory]
        [MemberData(nameof(PositiveTests))]
        public static void DecodeAndValidateUserInputTest(string seed, string pgpSeed)
        {
            var decodedSeed = WalletSeed.DecodeAndValidateUserInput(pgpSeed, PgpWordList);
            var decodedSeedHex = Hexadecimal.Decode(seed);
            Assert.Equal(decodedSeedHex, decodedSeed);
        }

        [Theory]
        [MemberData(nameof(NegativeTests))]
        public static void NegativeDecodeAndValidateUserInputTest(string pgpSeed, Type exception)
        {
            Assert.Throws(exception, () => WalletSeed.DecodeAndValidateUserInput(pgpSeed, PgpWordList));
        }
    }
}