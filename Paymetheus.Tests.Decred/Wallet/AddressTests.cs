// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Decred.Wallet
{
    public static class AddressTests
    {
        public static IEnumerable<object[]> ValidPayToSecp256k1PubKeyHashAddressTheories()
        {
            return new[]
            {
                new object[] { "DsR4PRmFaVNSu6SJ6ERM7rPZeGvKviny2e1", BlockChainIdentity.MainNet },
                new object[] { "DcXZ4zkDvDhJUqQ8tKu2KukVXzFo2R8PCwF", BlockChainIdentity.MainNet },
                new object[] { "TsmWaPM77WSyA3aiQ2Q1KnwGDVWvEkhipBc", BlockChainIdentity.TestNet },
            };
        }

        [Theory]
        [MemberData(nameof(ValidPayToSecp256k1PubKeyHashAddressTheories))]
        public static void ValidPayToSecp256k1PubKeyHashAddresses(string encodedAddress, BlockChainIdentity intendedBlockChain)
        {
            // Assert TryDecode and Decode succeed on valid address.
            Address address;
            Assert.True(Address.TryDecode(encodedAddress, out address));
            address = Address.Decode(encodedAddress);

            // Assert actual instance type is PayToSecp256k1PubKeyHash
            Assert.IsType<Address.PayToSecp256k1PubKeyHash>(address);
            var p2pkhAddress = (Address.PayToSecp256k1PubKeyHash)address;

            // Assert address is for the correct intended network.
            Assert.Same(address.IntendedBlockChain, intendedBlockChain);

            // Assert reencoded address string is equal to the test input.
            var newAddress = new Address.PayToSecp256k1PubKeyHash(intendedBlockChain, p2pkhAddress.PubKeyHash);
            var reencodedAddress = newAddress.Encode();
            Assert.Equal(encodedAddress, reencodedAddress);
        }

        public static IEnumerable<object[]> ValidPayToScriptHashAddressTheories()
        {
            return new[]
            {
                new object[] { "3QJmV3qfvL9SuYo34YihAf3sRCW3qSinyC", BlockChainIdentity.MainNet },
                new object[] { "3NukJ6fYZJ5Kk8bPjycAnruZkE5Q7UW7i8", BlockChainIdentity.MainNet },
                new object[] { "2NBFNJTktNa7GZusGbDbGKRZTxdK9VVez3n", BlockChainIdentity.TestNet },
            };
        }

        [Theory]
        [MemberData(nameof(ValidPayToScriptHashAddressTheories))]
        public static void ValidPayToScriptHashAddresses(string encodedAddress, BlockChainIdentity intendedBlockChain)
        {
            Address address;
            Assert.True(Address.TryDecode(encodedAddress, out address));
            address = Address.Decode(encodedAddress);

            Assert.IsType<Address.PayToScriptHash>(address);
            var p2shAddress = (Address.PayToScriptHash)address;

            Assert.Same(address.IntendedBlockChain, intendedBlockChain);

            var newAddress = new Address.PayToSecp256k1PubKeyHash(intendedBlockChain, p2shAddress.ScriptHash);
            var reencodedAddress = address.Encode();
            Assert.Equal(encodedAddress, reencodedAddress);
        }

        [Theory]
        [InlineData("1MirQ9bwyQcGVJPwKUgapu5ouK2E2Ey4gY")]
        public static void InvalidAddresses(string invalidEncodedAddress)
        {
            Address address;
            Assert.False(Address.TryDecode(invalidEncodedAddress, out address));
            Assert.Throws<AddressException>(() => Address.Decode(invalidEncodedAddress));
        }

        [Fact]
        public static void ThrowsForImproperlySizedHash160()
        {
            var tooShortHash160 = new byte[Ripemd160Hash.Length - 1];
            var tooLongHash160 = new byte[Ripemd160Hash.Length + 1];

            Assert.Throws<AddressException>(() => new Address.PayToSecp256k1PubKeyHash(BlockChainIdentity.MainNet, tooShortHash160));
            Assert.Throws<AddressException>(() => new Address.PayToSecp256k1PubKeyHash(BlockChainIdentity.MainNet, tooLongHash160));

            Assert.Throws<AddressException>(() => new Address.PayToScriptHash(BlockChainIdentity.MainNet, tooShortHash160));
            Assert.Throws<AddressException>(() => new Address.PayToScriptHash(BlockChainIdentity.MainNet, tooLongHash160));
        }

        [Fact]
        public static void NoThrowForProperlySizedHash160()
        {
            var correctLengthHash160 = new byte[Ripemd160Hash.Length];

            new Address.PayToSecp256k1PubKeyHash(BlockChainIdentity.MainNet, correctLengthHash160);
            new Address.PayToScriptHash(BlockChainIdentity.MainNet, correctLengthHash160);
        }
    }
}
