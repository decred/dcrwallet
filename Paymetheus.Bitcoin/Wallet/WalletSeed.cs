// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Util;
using System;
using System.Security.Cryptography;

namespace Paymetheus.Bitcoin.Wallet
{
    public static class WalletSeed
    {
        /// <summary>
        /// The length in bytes of the wallet's BIP0032 seed.
        /// </summary>
        public const int SeedLength = 32;

        public static byte[] GenerateRandomSeed()
        {
            var seed = new byte[SeedLength];
            using (var prng = new RNGCryptoServiceProvider())
            {
                prng.GetBytes(seed);
            }
            return seed;
        }

        /// <summary>
        /// Decodes user input as either the hexadecimal or PGP word list encoding
        /// of a seed and validates the seed length.
        /// </summary>
        public static byte[] DecodeAndValidateUserInput(string userInput, PgpWordList pgpWordList)
        {
            if (userInput == null)
                throw new ArgumentNullException(nameof(userInput));
            if (pgpWordList == null)
                throw new ArgumentNullException(nameof(pgpWordList));

            var decodedInput = DecodeUserInput(userInput, pgpWordList);
            if (decodedInput.Length != SeedLength)
            {
                throw new Exception($"Decoded seed must have byte length {SeedLength}");
            }
            return decodedInput;
        }

        private static byte[] DecodeUserInput(string userInput, PgpWordList pgpWordList)
        {
            byte[] seed;
            if (Hexadecimal.TryDecode(userInput, out seed))
                return seed;

            var splitInput = userInput.Split(new char[0], StringSplitOptions.RemoveEmptyEntries);
            if (splitInput.Length == 1)
            {
                // Hex decoding failed, but it's not a multi-word mneumonic either.
                // Assume the user intended hex.
                throw new HexadecimalEncodingException();
            }
            return pgpWordList.Decode(splitInput);
        }
    }
}
