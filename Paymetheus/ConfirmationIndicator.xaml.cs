// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Windows;
using System.Windows.Controls;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for ConfirmationIndicator.xaml
    /// </summary>
    public partial class ConfirmationIndicator : UserControl
    {
        public ConfirmationIndicator()
        {
            InitializeComponent();
            Render();
        }

        public const double Radius = 11;

        static readonly Size ArcSize = new Size(Radius, Radius);

        public int Confirmations
        {
            get { return (int)GetValue(ConfirmationsProperty); }
            set { SetValue(ConfirmationsProperty, value); }
        }

        public int RequiredConfirmations
        {
            get { return (int)GetValue(RequiredConfirmationsProperty); }
            set { SetValue(RequiredConfirmationsProperty, value); }
        }

        public static readonly DependencyProperty ConfirmationsProperty =
            DependencyProperty.Register(nameof(Confirmations), typeof(int), typeof(ConfirmationIndicator), new PropertyMetadata(0, new PropertyChangedCallback(OnPropertyChanged)));

        public static readonly DependencyProperty RequiredConfirmationsProperty =
            DependencyProperty.Register(nameof(RequiredConfirmations), typeof(int), typeof(ConfirmationIndicator), new PropertyMetadata(6, new PropertyChangedCallback(OnPropertyChanged)));

        private static void OnPropertyChanged(DependencyObject sender, DependencyPropertyChangedEventArgs args)
        {
            var indicator = (ConfirmationIndicator)sender;
            indicator.Render();
        }

        private void Render()
        {
            var confirms = Confirmations;
            var radians = 2 * Math.PI * confirms / RequiredConfirmations;
            var endPoint = CartesianCoordinate(radians);
            var largeArc = radians > Math.PI;

            arc.Point = endPoint;
            arc.Size = ArcSize;
            arc.IsLargeArc = largeArc;
            confirmationLabel.Content = confirms;
        }

        private static Point CartesianCoordinate(double radians)
        {
            var x = Radius * Math.Sin(radians);
            var y = Radius * Math.Cos(radians);

            return new Point(x, y);
        }
    }
}