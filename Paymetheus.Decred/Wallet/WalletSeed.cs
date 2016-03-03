// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;
using static PCLCrypto.WinRTCrypto;

namespace Paymetheus.Decred.Wallet
{
    public static class WalletSeed
    {
        /// <summary>
        /// The length in bytes of the wallet's BIP0032 seed.
        /// </summary>
        public const int SeedLength = 32;

        public static byte[] GenerateRandomSeed() => CryptographicBuffer.GenerateRandom(SeedLength);

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
