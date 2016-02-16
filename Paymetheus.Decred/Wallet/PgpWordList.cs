// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Linq;

namespace Paymetheus.Decred.Wallet
{
    /// <summary>
    /// Provides encoding and decoding methods to convert between raw byte streams
    /// and their PGP word list encodings.
    /// </summary>
    public sealed partial class PgpWordList
    {
        public PgpWordList()
        {
            // The WordList string is a raw string (@"") with the same raw newlines
            // used in the source file.  This will commonly be \r\n on Windows.  An
            // empty separator list removes both, but empty entries must also be 
            // removed to ignore the emtpy string between \r and \n.
            _alternatingWords = WordList.Split(new char[0], StringSplitOptions.RemoveEmptyEntries);
            _wordIndexes = new Dictionary<string, ushort>();
            for (ushort i = 0; i < (ushort)_alternatingWords.Length; i++)
            {
                _wordIndexes[CaseInsensitive(_alternatingWords[i])] = i;
            }
        }

        private string[] _alternatingWords;
        private Dictionary<string, ushort> _wordIndexes;

        // Strings must be normalized so character case is ignored during decoding.
        private static string CaseInsensitive(string word) => word.ToLower();

        // The word list is sorted by alternating the even and odd word lists.
        // For odd byte indexes, the array index must be incremented by one.
        private static int WordListIndex(byte b, int byteIndex) => b * 2 + byteIndex % 2;

        // Returns whether the word list index is invalid for the current byte index
        // (mismatching even and odd positions).
        private static bool MismatchedIndex(int byteIndex, ushort wordListIndex) =>
            (byteIndex & 0x01) != (wordListIndex & 0x01);

        // Convert a word index to the original byte.  The bit in the LSB position
        // (0 for even indexes, 1 for odd) does not need to be masked off as it is
        // removed by the integer division (actually a bit shift).
        private static byte OriginalByte(ushort wordIndex) => (byte)(wordIndex / 2);

        /// <summary>
        /// Create the PGP word list encoding of a byte stream.
        /// </summary>
        /// <param name="value">The byte stream to encode</param>
        /// <returns>The byte stream encoded using the PGP word list</returns>
        /// <exception cref="ArgumentNullException">The value argument is null</exception>
        public string[] Encode(byte[] value)
        {
            if (value == null)
                throw new ArgumentNullException(nameof(value));

            return value.Select((b, i) => _alternatingWords[WordListIndex(b, i)]).ToArray();
        }

        /// <summary>
        /// Creates the original byte stream for a PGP word list encoding.
        /// </summary>
        /// <param name="words">The encoded byte stream</param>
        /// <returns>The decoded byte stream</returns>
        /// <exception cref="ArgumentNullException">The words argument is null</exception>
        /// <exception cref="PgpWordListInvalidEncodingException">
        /// The words argument is not a valid PGP word list encoding
        /// </exception>
        public byte[] Decode(string[] words)
        {
            if (words == null)
                throw new ArgumentNullException(nameof(words));

            var result = new byte[words.Length];

            for (var byteIndex = 0; byteIndex < words.Length; byteIndex++)
            {
                var word = words[byteIndex];

                ushort wordListIndex;
                if (!_wordIndexes.TryGetValue(CaseInsensitive(word), out wordListIndex))
                {
                    throw PgpWordListInvalidEncodingException.UnrecognizedWord(word);
                }
                if (MismatchedIndex(byteIndex, wordListIndex))
                {
                    throw PgpWordListInvalidEncodingException.InvalidWordIndex(word, byteIndex);
                }

                result[byteIndex] = OriginalByte(wordListIndex);
            }

            return result;
        }
    }

    public class PgpWordListInvalidEncodingException : Exception
    {
        private PgpWordListInvalidEncodingException(string message) : base(message) { }

        public static PgpWordListInvalidEncodingException UnrecognizedWord(string word)
        {
            var message = $"Word `{word}` is not from the PGP word list.";
            return new PgpWordListInvalidEncodingException(message);
        }

        public static PgpWordListInvalidEncodingException InvalidWordIndex(string word, int index)
        {
            var message = $"Word `{word}` is not valid at position {index}.  Check for missing words.";
            return new PgpWordListInvalidEncodingException(message);
        }
    }
}
